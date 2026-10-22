//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

// Throwaway Windows CI diagnostic. It deliberately allows the shell to
// delete its fixtures permanently and only logs, including setup errors.
var (
	trashDiagGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
	trashDiagGetDiskFreeSpaceW   = kernel32.NewProc("GetDiskFreeSpaceW")
	trashDiagRegEnumValueW       = advapi32.NewProc("RegEnumValueW")
	trashDiagRegEnumKeyExW       = advapi32.NewProc("RegEnumKeyExW")
	trashDiagSinkOnce            sync.Once
	trashDiagSinkVtbl            [19]uintptr
	trashDiagLogs                = map[uintptr]*testing.T{}
)

func TestRecycleBinDiagnostics(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	comHR, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	t.Logf("CoInitializeEx: %#08x", uint32(comHR))
	if !failed(comHR) {
		defer procCoUninitialize.Call()
	}
	// t.TempDir would fail the test if setup failed; this test only logs.
	dir, err := os.MkdirTemp("", "mygo-recycle-diagnostics-")
	if err != nil {
		t.Logf("create TEMP directory: %v", err)
		return
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("remove diagnostic fixtures: %v", err)
		}
	})
	mount, volume, key, volumeErr := trashTestVolume(dir)
	t.Logf("TEMP=%q mount=%q volume=%q", dir, mount, volume)
	if volumeErr != nil {
		t.Log(volumeErr)
	}
	if mount != "" {
		var available, total, free uint64
		if r, _, e := trashDiagGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(u16(mount))), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free))); r == 0 {
			t.Logf("GetDiskFreeSpaceEx: %v", e)
		} else {
			t.Logf("volume bytes: total=%d free=%d available to caller=%d", total, free, available)
			const gib = uint64(1 << 30)
			first := min(total, 40*gib)
			t.Logf("predicted MaxCapacity (MiB): 10%% of first 40 GiB + 5%% of rest=%.3f; 5%% of volume=%.3f; 10%% of volume=%.3f",
				(float64(first)*0.10+float64(total-first)*0.05)/(1<<20), float64(total)*0.05/(1<<20), float64(total)*0.10/(1<<20))
		}
		var sectorsPerCluster, bytesPerSector, freeClusters, clusters uint32
		if r, _, e := trashDiagGetDiskFreeSpaceW.Call(uintptr(unsafe.Pointer(u16(mount))), uintptr(unsafe.Pointer(&sectorsPerCluster)), uintptr(unsafe.Pointer(&bytesPerSector)), uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&clusters))); r == 0 {
			t.Logf("GetDiskFreeSpace: %v", e)
		} else {
			t.Logf("cluster bytes=%d (sectors per cluster=%d, bytes per sector=%d)", uint64(sectorsPerCluster)*uint64(bytesPerSector), sectorsPerCluster, bytesPerSector)
		}
	}
	if key != "" {
		trashDiagDumpValues(t, key)
	}
	trashDiagDumpSubkeys(t, trashTestBitBucketKey+`\Volume`)
	trashDiagDumpValues(t, trashTestBitBucketKey)

	// Snapshot before even the first shell operation, so a key Windows
	// creates on first use is removed again if it was originally missing.
	canRestore := false
	if key != "" {
		if err := trashTestSaveBinSettings(t, key, t.Logf); err != nil {
			t.Logf("save original recycle bin settings: %v", err)
		} else {
			canRestore = true
		}
	}
	if canRestore {
		trashDiagChildren(t, dir, mount, key)
	} else {
		t.Log("child operations unavailable: registry snapshot failed")
	}
	t.Log("in-process diagnostics, with the original registry settings restored:")
	if !failed(comHR) && canRestore {
		path := filepath.Join(dir, "first-use.bin")
		if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
			t.Logf("create first-use file: %v", err)
		} else {
			t.Logf("normal TrashItem(1 byte): %v", (shell{}).TrashItem(path))
		}
	} else {
		t.Log("normal TrashItem unavailable: COM or registry snapshot failed")
	}
	t.Log("volume key after the normal TrashItem attempt:")
	if key != "" {
		trashDiagDumpValues(t, key)
	}
	if failed(comHR) || !canRestore {
		t.Log("logging operations unavailable: COM or registry snapshot failed")
		return
	}
	settingsOK := true
	for name, value := range map[string]uint32{"NukeOnDelete": 0, "MaxCapacity": 1} {
		if err := trashTestSetDWORD(key, name, value); err != nil {
			t.Logf("set %s=%d: %v", name, value, err)
			settingsOK = false
		}
	}
	if settingsOK {
		t.Log("logging operations with NukeOnDelete=0, MaxCapacity=1 MiB:")
		trashDiagDumpValues(t, key)
		for _, tc := range []struct {
			name  string
			files int // zero means a single file
			bytes int
		}{
			{"4-MiB-file", 0, 4 << 20},
			{"8-files-of-256-KiB", 8, 256 << 10},
			{"512-files-of-1025-bytes", 512, 1025},
			{"768-KiB-file", 0, 768 << 10},
		} {
			path := filepath.Join(dir, tc.name)
			if err := trashDiagMakeItem(path, tc.files, tc.bytes); err != nil {
				t.Logf("create %s: %v", tc.name, err)
				continue
			}
			trashDiagOperation(t, path, mount)
		}
	} else {
		t.Log("capacity cases unavailable: could not set the registry values")
	}
	if err := trashTestSetDWORD(key, "NukeOnDelete", 1); err != nil {
		t.Logf("set NukeOnDelete=1: %v", err)
		return
	}
	t.Log("logging operation with NukeOnDelete=1:")
	path := filepath.Join(dir, "nuke-on-delete.bin")
	if err := trashDiagMakeItem(path, 0, 1); err != nil {
		t.Logf("create NukeOnDelete control: %v", err)
	} else {
		trashDiagOperation(t, path, mount)
	}
}

func trashDiagChildren(t *testing.T, dir, mount, key string) {
	// Each subtest restores its settings before the next group, including
	// before the original in-process diagnostic's first TrashItem call.
	t.Run("children-capacity", func(t *testing.T) {
		if err := trashTestSaveBinSettings(t, key, t.Logf); err != nil {
			t.Logf("save recycle bin settings for children: %v", err)
			return
		}
		settingsOK := true
		for name, value := range map[string]uint32{"NukeOnDelete": 0, "MaxCapacity": 1} {
			if err := trashTestSetDWORD(key, name, value); err != nil {
				t.Logf("set %s=%d for children: %v", name, value, err)
				settingsOK = false
			}
		}
		if !settingsOK {
			t.Log("child capacity cases unavailable: could not set the registry values")
			return
		}
		t.Log("fresh child processes with NukeOnDelete=0, MaxCapacity=1 MiB:")
		trashDiagDumpValues(t, key)
		for _, tc := range []struct {
			name  string
			files int
			bytes int
		}{
			{"4-MiB-file", 0, 4 << 20},
			{"1048576-byte-file", 0, 1048576},
			{"1048575-byte-file", 0, 1048575},
			{"8-files-of-256-KiB", 8, 256 << 10},
			{"512-files-of-1025-bytes", 512, 1025},
			{"768-KiB-file", 0, 768 << 10},
		} {
			trashDiagRunPair(t, dir, mount, tc.name, tc.files, tc.bytes)
		}
	})
	t.Run("children-nuke-on-delete", func(t *testing.T) {
		if err := trashTestSaveBinSettings(t, key, t.Logf); err != nil {
			t.Logf("save recycle bin settings for children: %v", err)
			return
		}
		// The capacity subtest's cleanup has restored the original
		// MaxCapacity; change only NukeOnDelete for this control.
		if err := trashTestSetDWORD(key, "NukeOnDelete", 1); err != nil {
			t.Logf("set NukeOnDelete=1 for children: %v", err)
			return
		}
		t.Log("fresh child processes with NukeOnDelete=1 and the original MaxCapacity:")
		trashDiagDumpValues(t, key)
		trashDiagRunPair(t, dir, mount, "nuke-on-delete", 0, 1)
	})
}

func trashDiagRunPair(t *testing.T, dir, mount, name string, files, size int) {
	for _, mode := range []string{"logging-sink", "TrashItem"} {
		path := filepath.Join(dir, "child-"+name+"-"+mode)
		if err := trashDiagMakeItem(path, files, size); err != nil {
			t.Logf("create child fixture %s: %v", path, err)
			continue
		}
		t.Logf("fresh child: mode=%s path=%q", mode, path)
		trashDiagBin(t, mount, "before child")
		cmd := exec.Command(os.Args[0], "-test.run=^TestRecycleBinDiagnosticsChild$", "-test.v")
		cmd.Env = append(os.Environ(), "MYGO_TEST_RECYCLE_BIN_DIAGNOSTICS_CHILD="+path, "MYGO_TEST_RECYCLE_BIN_DIAGNOSTICS_MODE="+mode)
		out, err := cmd.CombinedOutput()
		t.Logf("child mode=%s exit=%v; full output:\n%s", mode, err, out)
		info, err := os.Stat(path)
		if err == nil {
			t.Logf("path after child: exists=true isDir=%t size=%d", info.IsDir(), info.Size())
		} else if os.IsNotExist(err) {
			t.Log("path after child: exists=false")
		} else {
			t.Logf("path after child: existence unavailable: %v", err)
		}
		trashDiagBin(t, mount, "after child")
	}
}

func TestRecycleBinDiagnosticsChild(t *testing.T) {
	path := os.Getenv("MYGO_TEST_RECYCLE_BIN_DIAGNOSTICS_CHILD")
	if path == "" {
		t.Skip("only run as a recycle bin diagnostic child")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); failed(r) {
		t.Log(hresultError("CoInitializeEx", r))
		return
	}
	defer procCoUninitialize.Call()
	mode := os.Getenv("MYGO_TEST_RECYCLE_BIN_DIAGNOSTICS_MODE")
	t.Logf("child pid=%d mode=%s path=%q", os.Getpid(), mode, path)
	switch mode {
	case "logging-sink":
		mount, _, _, err := trashTestVolume(path)
		if err != nil {
			t.Logf("child volume: %v", err)
			return
		}
		trashDiagOperation(t, path, mount)
	case "TrashItem":
		t.Logf("real TrashItem(%s): %v", path, (shell{}).TrashItem(path))
	default:
		t.Logf("unknown child mode %q", mode)
	}
}

func trashDiagMakeItem(path string, files, size int) error {
	if files == 0 {
		return os.WriteFile(path, make([]byte, size), 0o600)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return err
	}
	for i := range files {
		if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("%d.bin", i)), make([]byte, size), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func trashDiagDumpValues(t *testing.T, path string) {
	t.Logf("HKCU\\%s values:", path)
	var key uintptr
	if r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(path))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key))); r != 0 {
		if syscall.Errno(r) == syscall.ERROR_FILE_NOT_FOUND || syscall.Errno(r) == syscall.ERROR_PATH_NOT_FOUND {
			t.Log("  key missing")
		} else {
			t.Logf("  RegOpenKeyEx: %v", syscall.Errno(r))
		}
		return
	}
	defer procRegCloseKey.Call(key)
	name := make([]uint16, 256)
	data := make([]byte, 256)
	for i := uint32(0); ; {
		nameLen, dataLen := uint32(len(name)), uint32(len(data))
		var kind uint32
		r, _, _ := trashDiagRegEnumValueW.Call(key, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dataLen)))
		if syscall.Errno(r) == syscall.ERROR_MORE_DATA {
			name = make([]uint16, len(name)*2)
			data = make([]byte, max(len(data)*2, int(dataLen)))
			continue
		}
		if r == 259 { // ERROR_NO_MORE_ITEMS
			if i == 0 {
				t.Log("  (no values)")
			}
			return
		}
		if r != 0 {
			t.Logf("  RegEnumValue(%d): %v", i, syscall.Errno(r))
			return
		}
		value := data[:dataLen]
		extra := ""
		if kind == 4 && len(value) == 4 {
			extra = fmt.Sprintf(" (REG_DWORD %d)", binary.LittleEndian.Uint32(value))
		}
		t.Logf("  name=%q type=%d data=%x%s", syscall.UTF16ToString(name[:nameLen]), kind, value, extra)
		i++
	}
}

func trashDiagDumpSubkeys(t *testing.T, path string) {
	t.Logf("HKCU\\%s subkeys:", path)
	var key uintptr
	const keyEnumerateSubKeys = 0x0008
	if r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(path))), 0, keyEnumerateSubKeys, uintptr(unsafe.Pointer(&key))); r != 0 {
		if syscall.Errno(r) == syscall.ERROR_FILE_NOT_FOUND || syscall.Errno(r) == syscall.ERROR_PATH_NOT_FOUND {
			t.Log("  key missing")
		} else {
			t.Logf("  RegOpenKeyEx: %v", syscall.Errno(r))
		}
		return
	}
	defer procRegCloseKey.Call(key)
	var name [256]uint16
	for i := uint32(0); ; i++ {
		n := uint32(len(name))
		r, _, _ := trashDiagRegEnumKeyExW.Call(key, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)
		if r == 259 { // ERROR_NO_MORE_ITEMS
			if i == 0 {
				t.Log("  (no subkeys)")
			}
			return
		}
		if r != 0 {
			t.Logf("  RegEnumKeyEx(%d): %v", i, syscall.Errno(r))
			return
		}
		t.Logf("  %q", syscall.UTF16ToString(name[:n]))
	}
}

func trashDiagInitSink() {
	trashSinkOnce.Do(initTrashSink)
	trashDiagSinkVtbl = trashSinkVtbl
	trashDiagSinkVtbl[11] = syscall.NewCallback(func(this, flags, item uintptr) uintptr {
		t := trashDiagLogs[this]
		var p uintptr
		hr := comCall(item, siGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&p)))
		if failed(hr) || p == 0 {
			t.Logf("PreDeleteItem: flags=%#08x path unavailable (GetDisplayName=%#08x)", uint32(flags), uint32(hr))
		} else {
			t.Logf("PreDeleteItem: flags=%#08x path=%q", uint32(flags), wstr(p))
		}
		if p != 0 {
			procCoTaskMemFree.Call(p)
		}
		return sOK
	})
	trashDiagSinkVtbl[12] = syscall.NewCallback(func(this, flags, item, hrDelete, newlyCreated uintptr) uintptr {
		trashDiagLogs[this].Logf("PostDeleteItem: flags=%#08x hrDelete=%#08x psiNewlyCreatedIsNil=%t", uint32(flags), uint32(hrDelete), newlyCreated == 0)
		return sOK
	})
}

func trashDiagBin(t *testing.T, mount, when string) {
	info := struct {
		Size  uint32
		Bytes int64
		Items int64
	}{}
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, _ := procSHQueryRecycleBinW.Call(uintptr(unsafe.Pointer(u16(mount))), uintptr(unsafe.Pointer(&info))); failed(r) {
		t.Logf("recycle bin %s: SHQueryRecycleBin=%#08x", when, uint32(r))
	} else {
		t.Logf("recycle bin %s: items=%d bytes=%d", when, info.Items, info.Bytes)
	}
}

func trashDiagOperation(t *testing.T, path, mount string) {
	t.Logf("operation: %s", path)
	trashDiagBin(t, mount, "before")
	defer func() {
		info, err := os.Stat(path)
		if err == nil {
			t.Logf("path after: exists=true isDir=%t size=%d", info.IsDir(), info.Size())
		} else if os.IsNotExist(err) {
			t.Log("path after: exists=false")
		} else {
			t.Logf("path after: existence unavailable: %v", err)
		}
		trashDiagBin(t, mount, "after")
	}()
	trashDiagSinkOnce.Do(trashDiagInitSink)
	var item uintptr
	if r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item))); failed(r) || item == 0 {
		t.Logf("SHCreateItemFromParsingName: %#08x item=%#x", uint32(r), item)
		return
	}
	defer release(item)
	// Keep the original IUnknown callbacks and their trashSinks routing.
	// Release the operation before removing either sink-map entry.
	sink := &trashSink{vtbl: &trashDiagSinkVtbl, refs: 1}
	p := uintptr(unsafe.Pointer(sink))
	trashSinks[p] = sink
	trashDiagLogs[p] = t
	defer delete(trashSinks, p)
	defer delete(trashDiagLogs, p)
	var op uintptr
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidIFileOperation)), uintptr(unsafe.Pointer(&op))); failed(r) || op == 0 {
		t.Logf("CoCreateInstance(FileOperation): %#08x op=%#x", uint32(r), op)
		return
	}
	defer release(op)
	flags := uintptr(fofNoUI | fofAllowUndo | fofxRecycleOnDelete | fofxAddUndoRecord)
	t.Logf("SetOperationFlags=%#08x (same as TrashItem)", flags)
	if r := comCall(op, foSetOperationFlags, flags); failed(r) {
		t.Logf("SetOperationFlags: %#08x", uint32(r))
		return
	}
	if r := comCall(op, foDeleteItem, item, p); failed(r) {
		t.Logf("DeleteItem: %#08x", uint32(r))
		return
	}
	hr := comCall(op, foPerformOperations)
	var aborted int32
	abortHR := comCall(op, foGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
	t.Logf("PerformOperations=%#08x GetAnyOperationsAborted=%#08x aborted=%d", uint32(hr), uint32(abortHR), aborted)
}
