//go:build windows

package clip

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"time"
	"unsafe"

	core "clipsync/internal"

	"golang.org/x/sys/windows"
)

/*────── DLL and procedure loading (LazyDLL) ───────────────────*/
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procGetClipboardData         = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvail   = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procEnumClipboardFormats     = user32.NewProc("EnumClipboardFormats")
	procGetClipboardSequenceNum  = user32.NewProc("GetClipboardSequenceNumber")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

/*────── constants ────────────────────────────────────────────*/
const (
	CF_UNICODETEXT = 13
	CF_DIB         = 8
	GMEM_MOVEABLE  = 0x0002
)

var (
	fmtIDPng      uint32
	fmtIDImagePng uint32
)

func init() {
	fmtIDPng = regFormat("PNG")
	fmtIDImagePng = regFormat("image/png")
}

/*────── errors ───────────────────────────────────────────────*/
var (
	ErrClipboardBusy     = errors.New("clipboard busy")
	ErrUnsupportedFormat = errors.New("unsupported clipboard format")
	ErrBadDIB            = errors.New("malformed DIB")
)

/*────── API struct (build─tag windows) ─────────────────────*/
type ReqKind uint8

const (
	ReqRead  ReqKind = 0
	ReqWrite ReqKind = 1
)

type Req struct {
	Kind      ReqKind
	WantFmt   []uint32    // for reads (unused here)
	WriteData []core.Item // for writes
	Resp      chan Resp
}

type Resp struct {
	Items []core.Item
	Err   error
}

/*────── thread entry-point ──────────────────────────────────*/
// StartThread runs a goroutine that owns the clipboard.
// Returns the request channel.
func StartThread() chan<- Req {
	ch := make(chan Req)
	go clipThread(ch)
	return ch
}

func clipThread(in <-chan Req) {
	runtime.LockOSThread() // critical
	for req := range in {
		switch req.Kind {
		case ReqRead:
			items, err := readSnapshot()
			req.Resp <- Resp{Items: items, Err: err}
		case ReqWrite:
			err := writeSnapshot(req.WriteData)
			req.Resp <- Resp{Err: err}
		}
	}
}

/*────── low-level: open/close clipboard ──────────────────────*/
func openCB() error {
	start := time.Now()
	for {
		if ret, _, _ := procOpenClipboard.Call(0); ret != 0 {
			return nil
		}
		if time.Since(start) > 500*time.Millisecond {
			return ErrClipboardBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func closeCB() {
	procCloseClipboard.Call()
}

/*────── register a custom format like "PNG" ──────────────────*/
func regFormat(name string) uint32 {
	p, _ := windows.UTF16PtrFromString(name)
	ret, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(ret)
}

/*────── write snapshot (all items) ───────────────────────────*/
func writeSnapshot(items []core.Item) error {
	if err := openCB(); err != nil {
		return err
	}
	defer closeCB()

	procEmptyClipboard.Call()

	for _, it := range items {
		if it.Payload == "" {
			continue
		}
		payload, _ := base64.StdEncoding.DecodeString(it.Payload)

		switch it.Fmt {
		case CF_UNICODETEXT:
			if err := putText(string(payload)); err != nil {
				return err
			}
		case fmtIDPng, fmtIDImagePng:
			if err := putPNG(payload); err != nil {
				return err
			}
		}
	}
	return nil
}

// putPNG places a PNG on the clipboard (both CF_DIB and custom formats).
func putPNG(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}

	// put DIB
	dib := ImageToDIB(img)
	hDIB := hFromBytes(dib)
	ret, _, _ := procSetClipboardData.Call(CF_DIB, hDIB)
	if ret == 0 {
		return windows.GetLastError()
	}

	// put raw PNG as "PNG" and "image/png"
	hPNG := hFromBytes(data)
	if fmtIDPng != 0 {
		procSetClipboardData.Call(uintptr(fmtIDPng), hPNG)
	}
	if fmtIDImagePng != 0 {
		hPNG2 := hFromBytes(data) // can't reuse same handle
		procSetClipboardData.Call(uintptr(fmtIDImagePng), hPNG2)
	}
	return nil
}

// putText places UTF-16 text on the clipboard.
func putText(s string) error {
	utf16, _ := windows.UTF16FromString(s)
	size := 2 * len(utf16) // 2 bytes per UTF-16 code point
	h := alloc(size)
	p := lock(h)
	copy((*[1 << 30]uint16)(p)[:], utf16)
	procGlobalUnlock.Call(h)

	ret, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, h)
	if ret == 0 {
		return windows.GetLastError()
	}
	return nil
}

/*────── read snapshot ────────────────────────────────────────*/
func readSnapshot() ([]core.Item, error) {
	if err := openCB(); err != nil {
		return nil, err
	}
	defer closeCB()

	var items []core.Item

	// prioritize PNG formats
	if it := tryFormat(fmtIDPng, "PNG", "image/png"); it != nil {
		items = append(items, *it)
	} else if it := tryFormat(fmtIDImagePng, "image/png", "image/png"); it != nil {
		items = append(items, *it)
	} else if isAvail(CF_DIB) {
		if it := readDIBAsPNG(); it != nil {
			items = append(items, *it)
		}
	}

	// text fallback
	if isAvail(CF_UNICODETEXT) {
		if it := readText(); it != nil {
			items = append(items, *it)
		}
	}

	if len(items) == 0 {
		return nil, ErrUnsupportedFormat
	}
	return items, nil
}

// readDIBAsPNG converts CF_DIB -> PNG.
func readDIBAsPNG() *core.Item {
	h, _, _ := procGetClipboardData.Call(CF_DIB)
	if h == 0 {
		return nil
	}
	p := lock(uintptr(h))
	defer procGlobalUnlock.Call(h)

	size := globalSize(uintptr(h))
	dib := make([]byte, size)
	copy(dib, (*[1 << 30]byte)(p)[:size])

	png := DIBToPNG(dib)
	if png == nil {
		return nil
	}

	return &core.Item{
		Fmt:      CF_DIB,
		FmtName:  "PNG",
		MimeType: "image/png",
		Payload:  base64.StdEncoding.EncodeToString(png),
		ByteLen:  len(png),
	}
}

func readText() *core.Item {
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return nil
	}
	p := lock(uintptr(h))
	defer procGlobalUnlock.Call(h)

	var chars []uint16
	for i := 0; ; i++ {
		c := *(*uint16)(unsafe.Pointer(uintptr(p) + uintptr(i*2)))
		if c == 0 {
			break
		}
		chars = append(chars, c)
	}
	s := windows.UTF16ToString(chars)
	return &core.Item{
		Fmt:      CF_UNICODETEXT,
		FmtName:  "CF_UNICODETEXT",
		MimeType: "text/plain",
		Payload:  base64.StdEncoding.EncodeToString([]byte(s)),
		ByteLen:  len(s),
	}
}

// tryFormat attempts to read a custom clipboard format.
func tryFormat(fmt uint32, fmtName, mimeType string) *core.Item {
	if fmt == 0 || !isAvail(fmt) {
		return nil
	}
	h, _, _ := procGetClipboardData.Call(uintptr(fmt))
	if h == 0 {
		return nil
	}
	p := lock(uintptr(h))
	defer procGlobalUnlock.Call(h)

	size := globalSize(uintptr(h))
	data := make([]byte, size)
	copy(data, (*[1 << 30]byte)(p)[:size])

	return &core.Item{
		Fmt:      fmt,
		FmtName:  fmtName,
		MimeType: mimeType,
		Payload:  base64.StdEncoding.EncodeToString(data),
		ByteLen:  int(size),
	}
}

/*────── helpers ─────────────────────────────────────────────*/
func isAvail(fmt uint32) bool {
	ret, _, _ := procIsClipboardFormatAvail.Call(uintptr(fmt))
	return ret != 0
}

func alloc(size int) uintptr {
	h, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	return h
}

func lock(h uintptr) unsafe.Pointer {
	p, _, _ := procGlobalLock.Call(h)
	return unsafe.Pointer(p)
}

func hFromBytes(data []byte) uintptr {
	h := alloc(len(data))
	p := lock(h)
	copy((*[1 << 30]byte)(p)[:], data)
	procGlobalUnlock.Call(h)
	return h
}

func globalSize(h uintptr) int {
	ret, _, _ := kernel32.NewProc("GlobalSize").Call(h)
	return int(ret)
}

/*────── cheap sequence check ────────────────────────────────*/
func GetSeq() uint32 {
	seq, _, _ := procGetClipboardSequenceNum.Call()
	return uint32(seq)
}
