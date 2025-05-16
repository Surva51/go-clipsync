//go:build windows

package clip

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

/*────── test round─trip: Image → DIB → PNG ──────────────────*/
func TestImageToDIBToPNG(t *testing.T) {
	// create 10x10 checkerboard
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if (x+y)%2 == 0 {
				img.Set(x, y, color.RGBA{255, 0, 0, 255}) // red
			} else {
				img.Set(x, y, color.RGBA{0, 0, 255, 255}) // blue
			}
		}
	}

	// convert to DIB
	dib := ImageToDIB(img)
	if len(dib) < 40 {
		t.Fatalf("DIB too small: %d bytes", len(dib))
	}

	// convert back to PNG
	pngData := DIBToPNG(dib)
	if pngData == nil {
		t.Fatalf("DIBToPNG returned nil")
	}

	// decode PNG
	decoded, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}

	// verify dimensions
	if !decoded.Bounds().Eq(img.Bounds()) {
		t.Fatalf("bounds mismatch: got %v want %v",
			decoded.Bounds(), img.Bounds())
	}

	// spot-check pixels
	rgba, ok := decoded.(*image.RGBA)
	if !ok {
		t.Fatalf("decoded image not RGBA")
	}

	r1, g1, b1, a1 := rgba.At(0, 0).RGBA()
	r2, g2, b2, a2 := img.At(0, 0).RGBA()
	if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
		t.Fatalf("pixel (0,0) mismatch")
	}
}

func TestDIBWithAlpha(t *testing.T) {
	// create image with transparency
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})  // opaque red
	img.Set(1, 0, color.RGBA{0, 255, 0, 128})  // semi-transparent green
	img.Set(2, 0, color.RGBA{0, 0, 255, 64})   // mostly transparent blue
	img.Set(3, 0, color.RGBA{255, 255, 255, 0}) // fully transparent white

	dib := ImageToDIB(img)
	pngData := DIBToPNG(dib)

	decoded, _ := png.Decode(bytes.NewReader(pngData))
	rgba := decoded.(*image.RGBA)

	// check alpha values preserved
	_, _, _, a1 := rgba.At(0, 0).RGBA()
	_, _, _, a2 := rgba.At(1, 0).RGBA()
	_, _, _, a3 := rgba.At(2, 0).RGBA()
	_, _, _, a4 := rgba.At(3, 0).RGBA()

	if a1 != 0xffff || a2 < 0x7000 || a2 > 0x9000 ||
		a3 < 0x3000 || a3 > 0x5000 || a4 != 0 {
		t.Fatalf("alpha values not preserved")
	}
}
