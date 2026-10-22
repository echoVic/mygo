//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

var procSHQueryRecycleBinW = shell32.NewProc("SHQueryRecycleBinW")

func TestMain(m *testing.M) {
	if path := os.Getenv("MYGO_TEST_TRASH_ITEM"); path != "" {
		// Helper process for the tests of the recycle bin's size: the
		// shell reads its settings once per process, so TrashItem runs in
		// one started after the test has written them.
		runtime.LockOSThread()
		if r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); failed(r) {
			fmt.Fprintln(os.Stderr, hresultError("CoInitializeEx", r))
			os.Exit(1)
		}
		fmt.Println((shell{}).TrashItem(path))
		procCoUninitialize.Call()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// saveRecycleBinSettings registers restoration before the caller writes
// anything. Preserve the types and raw data too, even for non-DWORD values.
func saveRecycleBinSettings(t *testing.T, path string) error {
	t.Helper()
	var key uintptr
	r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(path))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key)))
	missing := syscall.Errno(r) == syscall.ERROR_FILE_NOT_FOUND || syscall.Errno(r) == syscall.ERROR_PATH_NOT_FOUND
	if r != 0 && !missing {
		return fmt.Errorf("RegOpenKeyEx(%s): %w", path, syscall.Errno(r))
	}
	if !missing {
		defer procRegCloseKey.Call(key)
	}
	values := []struct {
		name string
		kind uint32
		data []byte
	}{{name: "NukeOnDelete"}, {name: "MaxCapacity"}}
	if !missing {
		for i := range values {
			v := &values[i]
			var size uint32
			r, _, _ := procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(v.name))), 0, uintptr(unsafe.Pointer(&v.kind)), 0, uintptr(unsafe.Pointer(&size)))
			if syscall.Errno(r) == syscall.ERROR_FILE_NOT_FOUND {
				continue
			}
			if r != 0 {
				return fmt.Errorf("RegQueryValueEx(%s): %w", v.name, syscall.Errno(r))
			}
			v.data = make([]byte, size)
			if size > 0 {
				r, _, _ = procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(v.name))), 0, uintptr(unsafe.Pointer(&v.kind)), uintptr(unsafe.Pointer(&v.data[0])), uintptr(unsafe.Pointer(&size)))
				if r != 0 {
					return fmt.Errorf("RegQueryValueEx(%s): %w", v.name, syscall.Errno(r))
				}
				v.data = v.data[:size]
			}
		}
	}
	t.Cleanup(func() {
		if missing {
			if err := regDeleteTree(hkeyCurrentUser, path); err != nil {
				t.Errorf("restore missing recycle bin key: %v", err)
			}
			return
		}
		for _, v := range values {
			var err error
			if v.data != nil {
				err = writeRegistryValue(path, v.name, v.kind, v.data)
			} else {
				err = regDeleteValue(hkeyCurrentUser, path, v.name)
			}
			if err != nil {
				t.Errorf("restore recycle bin value %s: %v", v.name, err)
			}
		}
	})
	return nil
}

func writeRegistryValue(path, name string, kind uint32, data []byte) error {
	var key uintptr
	if r, _, _ := procRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(path))), 0, 0, 0, keySetValue, 0, uintptr(unsafe.Pointer(&key)), 0); r != 0 {
		return fmt.Errorf("RegCreateKeyEx(%s): %w", path, syscall.Errno(r))
	}
	defer procRegCloseKey.Call(key)
	var p *byte
	if len(data) > 0 {
		p = &data[0]
	}
	if r, _, _ := procRegSetValueExW.Call(key, uintptr(unsafe.Pointer(u16(name))), 0, uintptr(kind), uintptr(unsafe.Pointer(p)), uintptr(len(data))); r != 0 {
		return fmt.Errorf("RegSetValueEx(%s): %w", name, syscall.Errno(r))
	}
	return nil
}

func setRegistryDWORD(path, name string, value uint32) error {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	return writeRegistryValue(path, name, 4 /* REG_DWORD */, data[:])
}

// setRecycleBinCapacity sizes the recycle bin of path's volume, in MB, until
// the test ends.
func setRecycleBinCapacity(t *testing.T, path string, capacity uint32) {
	t.Helper()
	key, ok := recycleBinKey(path)
	if !ok {
		t.Fatalf("cannot find the recycle bin key for %s", path)
	}
	if err := saveRecycleBinSettings(t, key); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]uint32{"NukeOnDelete": 0, "MaxCapacity": capacity} {
		if err := setRegistryDWORD(key, name, value); err != nil {
			t.Fatal(err)
		}
	}
}

// trashItemInHelper runs TrashItem in a helper process (see TestMain) and
// returns what it printed: its error, or <nil>.
func trashItemInHelper(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MYGO_TEST_TRASH_ITEM="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TrashItem helper for %s: %v\n%s", path, err, out)
	}
	return strings.TrimSpace(string(out))
}

// recycled returns how many items the recycle bin of path's drive holds.
func recycled(t *testing.T, path string) int64 {
	t.Helper()
	info := struct {
		Size  uint32
		Bytes int64
		Items int64
	}{}
	info.Size = uint32(unsafe.Sizeof(info))
	root := filepath.VolumeName(path) + `\`
	if r, _, _ := procSHQueryRecycleBinW.Call(uintptr(unsafe.Pointer(u16(root))), uintptr(unsafe.Pointer(&info))); failed(r) {
		t.Fatalf("SHQueryRecycleBin(%s): %#x", root, r)
	}
	return info.Items
}

func TestTrashItem(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, coinitApartmentThreaded)
	dir := t.TempDir()

	file := filepath.Join(dir, "file.txt")
	folder := filepath.Join(dir, "folder")
	os.WriteFile(file, []byte("trash me"), 0o644)
	os.MkdirAll(filepath.Join(folder, "inner"), 0o755)
	os.WriteFile(filepath.Join(folder, "inner", "a.txt"), []byte("a"), 0o644)
	for _, path := range []string{file, folder} {
		before := recycled(t, path)
		if err := (shell{}).TrashItem(path); err != nil {
			t.Fatalf("TrashItem(%s): %v", path, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", path, err)
		}
		if after := recycled(t, path); after != before+1 {
			t.Errorf("TrashItem(%s): the recycle bin went from %d to %d items, not to the recycle bin", path, before, after)
		}
	}

	if err := (shell{}).TrashItem(filepath.Join(dir, "missing")); err == nil {
		t.Error("TrashItem of a missing file did not fail")
	}

	// A share has no recycle bin: Windows would delete the file for good,
	// so TrashItem fails and leaves it.
	kept := filepath.Join(dir, "kept.txt")
	os.WriteFile(kept, []byte("keep me"), 0o644)
	vol := filepath.VolumeName(kept)
	unc := `\\localhost\` + strings.TrimSuffix(vol, ":") + `$` + strings.TrimPrefix(kept, vol)
	if _, err := os.Stat(unc); err != nil {
		t.Skipf("no administrative share for %s: %v", vol, err)
	}
	if err := (shell{}).TrashItem(unc); err == nil {
		t.Errorf("TrashItem(%s) did not fail", unc)
	} else {
		t.Log(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("TrashItem(%s) deleted the file: %v", unc, err)
	}
}

func TestTrashItemKeepsAnItemTooBigForTheRecycleBin(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); failed(r) {
		t.Fatal(hresultError("CoInitializeEx", r))
	}
	defer procCoUninitialize.Call()
	dir := t.TempDir()
	setRecycleBinCapacity(t, dir, 1)

	file := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(file, make([]byte, 4<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := os.WriteFile(filepath.Join(folder, fmt.Sprintf("%d.bin", i)), make([]byte, 2<<20), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{file, folder} {
		before := recycled(t, path)
		if result := trashItemInHelper(t, path); result == "" || result == "<nil>" {
			t.Errorf("TrashItem(%s) reported %q, want an error for an item bigger than the 1 MiB recycle bin", path, result)
		} else {
			t.Logf("TrashItem(%s): %s", path, result)
		}
		var size int64
		err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				size += info.Size()
			}
			return nil
		})
		if err != nil {
			t.Errorf("TrashItem(%s) did not keep the item at its path: %v", path, err)
		} else if size != 4<<20 {
			t.Errorf("TrashItem(%s) left %d bytes, want %d", path, size, 4<<20)
		}
		if after := recycled(t, path); after != before {
			t.Errorf("TrashItem(%s): the recycle bin went from %d to %d items, want unchanged", path, before, after)
		}
	}
}

func TestTrashItemRecyclesAFileAsBigAsTheRecycleBin(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); failed(r) {
		t.Fatal(hresultError("CoInitializeEx", r))
	}
	defer procCoUninitialize.Call()
	dir := t.TempDir()
	setRecycleBinCapacity(t, dir, 1)
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	before := recycled(t, path)
	if result := trashItemInHelper(t, path); result != "<nil>" {
		t.Errorf("TrashItem(%s) reported %q, want <nil> for an item that fits the recycle bin", path, result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("TrashItem(%s) did not remove the item: %v", path, err)
	}
	if after := recycled(t, path); after != before+1 {
		t.Errorf("TrashItem(%s): the recycle bin went from %d to %d items, want one more", path, before, after)
	}
}

func TestDefaultRecycleBinCapacity(t *testing.T) {
	for _, tc := range []struct {
		total uint64
		want  uint64 // MiB
	}{
		{0, 0},
		{10 << 30, 1024},
		{40 << 30, 4096},
		{160939618304, 9722},
		{274284392448, 15126},
	} {
		if got := defaultRecycleBinCapacity(tc.total); got != tc.want {
			t.Errorf("defaultRecycleBinCapacity(%d) = %d MiB, want %d", tc.total, got, tc.want)
		}
	}
}
