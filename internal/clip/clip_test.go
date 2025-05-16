//go:build !windows

package clip

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
	"testing"

	core "clipsync/internal"
)

/*────── stub clipboard for non-Windows ───────────────────────*/
var stubData []core.Item

func GetSeq() uint32        { return 42 }        // dummy
func StartThread() chan Req { return make(chan Req) } // no-op

func writeSnapshot(items []core.Item) error {
	stubData = items
	return nil
}

func readSnapshot() ([]core.Item, error) {
	return stubData, nil
}

/*────── actual tests ──────────────────────────────────────────*/
func TestReadWrite(t *testing.T) {
	want := []core.Item{{
		Fmt:     1,
		Payload: base64.StdEncoding.EncodeToString([]byte("hello")),
		ByteLen: 5,
	}}

	if err := writeSnapshot(want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readSnapshot()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].Payload != want[0].Payload {
		t.Fatalf("mismatch: got %+v want %+v", got, want)
	}
}

/*────── image round─trip in stub mode ──────────────────────*/
func TestImageRoundTrip(t *testing.T) {
	// create a 2x2 test image
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, image.Black)
	img.Set(1, 0, image.White)
	img.Set(0, 1, image.White)
	img.Set(1, 1, image.Black)

	// encode to PNG
	var buf bytes.Buffer
	png.Encode(&buf, img)
	pngData := buf.Bytes()

	items := []core.Item{{
		Fmt:      8, // CF_DIB
		FmtName:  "PNG",
		MimeType: "image/png",
		Payload:  base64.StdEncoding.EncodeToString(pngData),
		ByteLen:  len(pngData),
	}}

	if err := writeSnapshot(items); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readSnapshot()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}

	// decode and verify
	payload, _ := base64.StdEncoding.DecodeString(got[0].Payload)
	decoded, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !decoded.Bounds().Eq(img.Bounds()) {
		t.Fatalf("bounds mismatch")
	}
}
