//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/transfer"
)

func TestClipboardDIBRejectsInvalidGeometry(t *testing.T) {
	for _, dimensions := range [][2]uint32{{0, 1}, {1, 0}, {0x7fffffff, 0x7fffffff}, {2, 0x80000000}, {1 << 20, 1 << 20}} {
		dib := make([]byte, 40)
		binary.LittleEndian.PutUint32(dib, 40)
		binary.LittleEndian.PutUint32(dib[4:], dimensions[0])
		binary.LittleEndian.PutUint32(dib[8:], dimensions[1])
		binary.LittleEndian.PutUint16(dib[12:], 1)
		binary.LittleEndian.PutUint16(dib[14:], 32)
		if img := dibToImage(dib); img != nil {
			t.Fatalf("malformed dimensions accepted: %v", dimensions)
		}
	}
	dib := imageToDIB(image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	if img := dibToImage(dib); img == nil || img.Bounds() != image.Rect(0, 0, 2, 3) {
		t.Fatal("valid top-down DIB rejected")
	}
	binary.LittleEndian.PutUint32(dib, 0)
	if dibToImage(dib) != nil {
		t.Fatal("invalid bitmap header accepted")
	}
}

// A representation offered but unreadable, as the EnterpriseDataProtectionId
// Notepad offers with no data behind it, costs a read of everything offered
// nothing more than itself: the text still reads (egoist/kero#286).
func TestClipboardReadDataSkipsUnreadable(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// A window of a predefined class owns the clipboard and renders nothing
	// when asked for the representation it only promised.
	hwnd := createWindow(0, "STATIC", "", 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("cannot create the clipboard's owner")
	}
	defer procDestroyWindow.Call(hwnd)
	c := clipboard{&Backend{appHwnd: hwnd}}
	if !c.open() {
		t.Fatal("cannot open the clipboard")
	}
	procEmptyClipboard.Call()
	if !setClipboardData(cfUnicodeText, utf16Bytes("KERO_TEST_123")) {
		closeClipboard()
		t.Fatal("cannot put text on the clipboard")
	}
	// Promised, for the owner to render later (delayed rendering), which
	// it never does. Formats below tells whether the promise was taken.
	id := registerClipboardFormat("EnterpriseDataProtectionId")
	procSetClipboardData.Call(uintptr(id), 0)
	closeClipboard()
	defer func() {
		if c.open() {
			procEmptyClipboard.Call()
			closeClipboard()
		}
	}()
	if !slices.Contains(c.Formats(), "EnterpriseDataProtectionId") {
		t.Fatalf("the promised representation is not offered: %q", c.Formats())
	}
	d, err := c.ReadData(nil)
	if err != nil {
		t.Fatalf("reading everything offered: %v", err)
	}
	if text, _ := d.Read(transfer.Text); string(text) != "KERO_TEST_123" {
		t.Fatalf("text %q, formats %q", text, d.Formats())
	}
	// Asked for by name, the unreadable representation is still an error.
	if _, err := c.ReadData([]transfer.Format{"EnterpriseDataProtectionId"}); err == nil {
		t.Fatal("reading the unreadable representation by name succeeded")
	}
}

func TestClipboardConcealedFormat(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dropOnce.Do(initDropTarget)
	mark := uint16(registerClipboardFormat(winConcealedFormat))
	data := transfer.TextData("hunter2")
	for _, c := range []struct{ clipboard, concealed, marked bool }{{false, false, false}, {true, false, false}, {true, true, true}} {
		object := newTransferData(data, "", c.clipboard, c.concealed)
		if got := slices.Contains(dragObjects[object].formats, mark); got != c.marked {
			t.Fatalf("clipboard %t, concealed %t: marked %t", c.clipboard, c.concealed, got)
		}
		if c.marked {
			if _, err := oleDataBytes(object, mark); err != nil {
				t.Fatalf("mark data: %v", err)
			}
		}
		release(object)
	}
}

// alphaTestImage is a pixel that is fully transparent and one that is half
// transparent red.
func alphaTestImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, A: 128})
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An image copied to the clipboard keeps its transparency in CF_DIBV5, and
// apps that read only CF_DIB, as Paint, get it on white rather than black.
func TestClipboardImageBitmaps(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dropOnce.Do(initDropTarget)
	src := alphaTestImage()
	object := newTransferData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBytes(t, src)))), "", true, false)
	defer release(object)
	formats := dragObjects[object].formats
	v5, dib := slices.Index(formats, cfDIBV5), slices.Index(formats, cfDIB)
	if v5 < 0 || dib < 0 || v5 > dib {
		t.Fatalf("bitmaps offered %v, want CF_DIBV5 before CF_DIB", formats)
	}
	b, err := oleDataBytes(object, cfDIBV5)
	if err != nil {
		t.Fatal(err)
	}
	if img, ok := dibToImage(b).(*image.NRGBA); !ok || img.NRGBAAt(0, 0).A != 0 || img.NRGBAAt(1, 0) != src.NRGBAAt(1, 0) {
		t.Fatalf("CF_DIBV5 lost transparency: %v", dibToImage(b))
	}
	if b, err = oleDataBytes(object, cfDIB); err != nil {
		t.Fatal(err)
	}
	img, ok := dibToImage(b).(*image.NRGBA)
	if !ok || img.NRGBAAt(0, 0) != (color.NRGBA{255, 255, 255, 255}) || img.NRGBAAt(1, 0) != (color.NRGBA{255, 127, 127, 255}) {
		t.Fatalf("CF_DIB is not the image on white: %v", img)
	}
}

// Apps that paste CF_BITMAP get the image on white too. Without a CF_BITMAP
// of its own, Windows makes one from CF_DIBV5 and drops the alpha, which
// shows transparent pixels black.
func TestClipboardImageGDIBitmap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dropOnce.Do(initDropTarget)
	object := newTransferData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBytes(t, alphaTestImage())))), "", true, false)
	defer release(object)
	var offered []formatEtc
	var enum uintptr
	if comCall(object, dataEnumFormatEtc, 1, uintptr(unsafe.Pointer(&enum))) != sOK {
		t.Fatal("formats not enumerated")
	}
	for {
		var f formatEtc
		if comCall(enum, 3, 1, uintptr(unsafe.Pointer(&f)), 0) != sOK {
			break
		}
		offered = append(offered, f)
	}
	release(enum)
	format := formatEtc{Format: cfBitmap, Aspect: dvaspectContent, Index: -1, Tymed: tymedGDI}
	if !slices.Contains(offered, format) {
		t.Fatalf("CF_BITMAP not offered as a GDI bitmap: %+v", offered)
	}
	if comCall(object, dataQueryGetData, uintptr(unsafe.Pointer(&format))) != sOK {
		t.Fatal("CF_BITMAP not available")
	}
	memory := format
	memory.Tymed = tymedHGlobal
	if comCall(object, dataQueryGetData, uintptr(unsafe.Pointer(&memory))) == sOK {
		t.Fatal("CF_BITMAP available in memory")
	}
	var m stgMedium
	if comCall(object, dataGetData, uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&m))) != sOK || m.Tymed != tymedGDI || m.Handle == 0 {
		t.Fatalf("CF_BITMAP not rendered: %+v", m)
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&m)))
	hdr := bitmapInfoHeader{Width: 2, Height: -1, Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	px := make([]byte, 8)
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	if r, _, _ := gdi32.NewProc("GetDIBits").Call(dc, m.Handle, 0, 1, uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&hdr)), 0); r != 1 {
		t.Fatal("CF_BITMAP has no pixels")
	}
	// Blue, green, red: white, then half transparent red on white.
	if got := [6]byte{px[0], px[1], px[2], px[4], px[5], px[6]}; got != [6]byte{255, 255, 255, 127, 127, 255} {
		t.Fatalf("CF_BITMAP pixels %v, want the image on white", px)
	}
}

// A bitmap's alpha counts only when its header says the pixels have one,
// and a bitmap whose alpha is zero everywhere is opaque: apps leave the
// fourth byte of their pixels zero.
func TestClipboardDIBAlpha(t *testing.T) {
	src := alphaTestImage()
	v5 := imageToDIBV5(src)
	if img, ok := dibToImage(v5).(*image.NRGBA); !ok || img.NRGBAAt(1, 0) != src.NRGBAAt(1, 0) {
		t.Fatal("alpha of a BITMAPV5HEADER lost")
	}
	noMask := slices.Clone(v5)
	binary.LittleEndian.PutUint32(noMask[52:], 0)
	if img, ok := dibToImage(noMask).(*image.NRGBA); !ok || img.NRGBAAt(0, 0).A != 255 {
		t.Fatal("a bitmap without an alpha mask read as transparent")
	}
	blank := imageToDIBV5(image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	if img, ok := dibToImage(blank).(*image.NRGBA); !ok || img.NRGBAAt(1, 1).A != 255 {
		t.Fatal("a bitmap with no alpha anywhere read as transparent")
	}
}

// With only a bitmap on the clipboard, as some screenshot tools leave it,
// ReadImage reads CF_DIBV5 and keeps the transparency.
func TestClipboardReadImageFromDIBV5(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := createWindow(0, "STATIC", "", 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("cannot create the clipboard's owner")
	}
	defer procDestroyWindow.Call(hwnd)
	c := clipboard{&Backend{appHwnd: hwnd}}
	if !c.open() {
		t.Fatal("cannot open the clipboard")
	}
	procEmptyClipboard.Call()
	src := alphaTestImage()
	if !setClipboardData(cfDIBV5, imageToDIBV5(src)) {
		closeClipboard()
		t.Fatal("cannot put a bitmap on the clipboard")
	}
	closeClipboard()
	defer func() {
		if c.open() {
			procEmptyClipboard.Call()
			closeClipboard()
		}
	}()
	img, err := png.Decode(bytes.NewReader(c.ReadImage()))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(1, 0)); got != src.NRGBAAt(1, 0) {
		t.Fatalf("pixel %v, want %v", got, src.NRGBAAt(1, 0))
	}
}
