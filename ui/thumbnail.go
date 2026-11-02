package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
)

// thumbnailMaxBytes bounds the file a thumbnail is made from. It is the same cap
// the transcript uses for a drawn picture: a larger file was never going to be
// drawn inline anyway.
const thumbnailMaxBytes = inlineImageMaxBytes

// thumbnailPNG reads a picture from path and returns a PNG scaled to height,
// or nil when the file is not a picture this build can decode.
//
// It exists for the sender's row: a phone's picker hands over a content:// URI
// with no path to read later, so the row has nothing to draw from once the
// staged copy is gone. A thumbnail is enough — the row draws at 180 px — and it
// is what keeps a sent photo from holding a full decoded bitmap for the life of
// the transcript (#303).
func thumbnailPNG(path string, height int) []byte {
	if height <= 0 {
		height = inlineImageHeight
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > thumbnailMaxBytes {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil
	}

	bounds := src.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil
	}

	width := bounds.Dx() * height / bounds.Dy()
	if width <= 0 {
		width = 1
	}

	small := image.NewRGBA(image.Rect(0, 0, width, height))
	scaleDown(small, src)

	var out bytes.Buffer
	if err := png.Encode(&out, small); err != nil {
		return nil
	}

	return out.Bytes()
}

// scaleDown averages each destination pixel over the part of the source it
// covers.
//
// Averaging rather than sampling keeps a photo of a page readable at 180 px;
// nearest-neighbour is cheaper but turns text into noise, which is exactly the
// case a chat preview is used for.
func scaleDown(dst *image.RGBA, src image.Image) {
	dstBounds := dst.Bounds()
	srcBounds := src.Bounds()

	for y := dstBounds.Min.Y; y < dstBounds.Max.Y; y++ {
		y0 := srcBounds.Min.Y + (y-dstBounds.Min.Y)*srcBounds.Dy()/dstBounds.Dy()
		y1 := srcBounds.Min.Y + (y-dstBounds.Min.Y+1)*srcBounds.Dy()/dstBounds.Dy()

		if y1 <= y0 {
			y1 = y0 + 1
		}

		for x := dstBounds.Min.X; x < dstBounds.Max.X; x++ {
			x0 := srcBounds.Min.X + (x-dstBounds.Min.X)*srcBounds.Dx()/dstBounds.Dx()
			x1 := srcBounds.Min.X + (x-dstBounds.Min.X+1)*srcBounds.Dx()/dstBounds.Dx()

			if x1 <= x0 {
				x1 = x0 + 1
			}

			var r, g, b, a, n uint64

			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sr, sg, sb, sa := src.At(sx, sy).RGBA()
					r += uint64(sr)
					g += uint64(sg)
					b += uint64(sb)
					a += uint64(sa)
					n++
				}
			}

			if n == 0 {
				continue
			}

			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r / n >> 8),
				G: uint8(g / n >> 8),
				B: uint8(b / n >> 8),
				A: uint8(a / n >> 8),
			})
		}
	}
}
