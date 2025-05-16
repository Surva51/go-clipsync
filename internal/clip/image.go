//go:build windows

package clip

import (
    "bytes"
    "encoding/binary"
    "image"
    "image/draw"
    "image/png"
)

/*───── ImageToDIB: converts image.Image → 40-byte DIB ───────────*/
func ImageToDIB(img image.Image) []byte {
    // ensure RGBA
    b := img.Bounds()
    rgba := image.NewRGBA(b)
    draw.Draw(rgba, b, img, image.Point{}, draw.Src)

    width := b.Dx()
    height := b.Dy()
    stride := ((width*4 + 3) / 4) * 4 // round up to DWORD

    // BITMAPINFOHEADER (40 bytes)
    hdr := make([]byte, 40)
    binary.LittleEndian.PutUint32(hdr[0:4], 40)       // biSize
    binary.LittleEndian.PutUint32(hdr[4:8], uint32(width))
    binary.LittleEndian.PutUint32(hdr[8:12], uint32(height))
    binary.LittleEndian.PutUint16(hdr[12:14], 1)      // biPlanes
    binary.LittleEndian.PutUint16(hdr[14:16], 32)     // biBitCount
    binary.LittleEndian.PutUint32(hdr[16:20], 0)      // biCompression (BI_RGB)
    binary.LittleEndian.PutUint32(hdr[20:24], uint32(stride*height))
    // Rest left at 0

    var buf bytes.Buffer
    buf.Write(hdr)

    // pixels bottom-up, BGRA
    rowBuf := make([]byte, stride)
    for y := height - 1; y >= 0; y-- {
        rowPtr := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]
        for x := 0; x < width; x++ {
            rowBuf[x*4+0] = rowPtr[x*4+2] // B
            rowBuf[x*4+1] = rowPtr[x*4+1] // G
            rowBuf[x*4+2] = rowPtr[x*4+0] // R
            rowBuf[x*4+3] = rowPtr[x*4+3] // A
        }
        // pad remainder
        for i := width * 4; i < stride; i++ {
            rowBuf[i] = 0
        }
        buf.Write(rowBuf)
    }

    return buf.Bytes()
}

/*───── DIBToPNG: converts DIB bytes → PNG bytes ───────────────*/
func DIBToPNG(dib []byte) []byte {
    if len(dib) < 40 {
        return nil
    }

    biSize := binary.LittleEndian.Uint32(dib[0:4])
    if biSize < 40 {
        return nil
    }

    width := int(binary.LittleEndian.Uint32(dib[4:8]))
    height := int(binary.LittleEndian.Uint32(dib[8:12]))
    bitCount := binary.LittleEndian.Uint16(dib[14:16])

    if bitCount != 32 {
        return nil // only 32-bit supported
    }

    bottomUp := height > 0
    if height < 0 {
        height = -height // top-down
    }

    pixelOffset := int(biSize)
    if len(dib) < pixelOffset {
        return nil
    }

    stride := ((width*4 + 3) / 4) * 4
    rgba := image.NewRGBA(image.Rect(0, 0, width, height))

    for y := 0; y < height; y++ {
        srcY := y
        if bottomUp {
            srcY = height - 1 - y
        }

        srcStart := pixelOffset + srcY*stride
        if srcStart+width*4 > len(dib) {
            break
        }

        dstRow := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]
        srcRow := dib[srcStart : srcStart+width*4]

        for x := 0; x < width; x++ {
            dstRow[x*4+0] = srcRow[x*4+2] // R
            dstRow[x*4+1] = srcRow[x*4+1] // G
            dstRow[x*4+2] = srcRow[x*4+0] // B
            dstRow[x*4+3] = srcRow[x*4+3] // A
        }
    }

    var buf bytes.Buffer
    if err := png.Encode(&buf, rgba); err != nil {
        return nil
    }
    return buf.Bytes()
}
