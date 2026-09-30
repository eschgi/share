package thumbs

import (
	"image"
	"image/color"
)

// fit returns the size of a w×h image scaled down to fit into side×side. Smaller images keep
// their size.
func fit(w, h, side int) (int, int) {
	switch {
	case w <= side && h <= side:
		return w, h
	case w >= h:
		return side, max(1, int(int64(h)*int64(side)/int64(w)))
	default:
		return max(1, int(int64(w)*int64(side)/int64(h))), side
	}
}

// shrink scales src down to w×h by averaging the source pixels that fall into each target
// pixel. That is sharp enough for thumbnails, and besides the result it needs only one sum
// per target pixel. Transparent parts come out black.
func shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	sums := make([]uint32, w*h*4) // red, green, blue, count
	col := make([]int, sw)        // target column of each source column
	for x := range col {
		col[x] = x * w / sw * 4
	}
	for y := range sh {
		row := sums[y*h/sh*w*4:]
		sy := b.Min.Y + y
		switch s := src.(type) {
		case *image.YCbCr: // JPEG photos: by far the most common, so without interface calls
			for x := range sw {
				sx := b.Min.X + x
				yi, ci := s.YOffset(sx, sy), s.COffset(sx, sy)
				r, g, bl := color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
				p := row[col[x] : col[x]+4]
				p[0] += uint32(r)
				p[1] += uint32(g)
				p[2] += uint32(bl)
				p[3]++
			}
		case *image.NRGBA: // PNG with transparency
			for x := range sw {
				c := s.Pix[s.PixOffset(b.Min.X+x, sy):]
				a := uint32(c[3])
				p := row[col[x] : col[x]+4]
				p[0] += uint32(c[0]) * a / 255
				p[1] += uint32(c[1]) * a / 255
				p[2] += uint32(c[2]) * a / 255
				p[3]++
			}
		default:
			for x := range sw {
				r, g, bl, _ := src.At(b.Min.X+x, sy).RGBA() // 16 bits, alpha already applied
				p := row[col[x] : col[x]+4]
				p[0] += r >> 8
				p[1] += g >> 8
				p[2] += bl >> 8
				p[3]++
			}
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		s := sums[i*4 : i*4+4]
		n := max(s[3], 1)
		dst.Pix[i*4], dst.Pix[i*4+1], dst.Pix[i*4+2], dst.Pix[i*4+3] =
			uint8(s[0]/n), uint8(s[1]/n), uint8(s[2]/n), 255
	}
	return dst
}

// orient turns img the way an EXIF orientation says, so it shows upright. Orientations 5 to 8
// swap width and height.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored upside down
				dx, dy = x, h-1-y
			case 5: // mirrored, lying on its left side
				dx, dy = y, x
			case 6: // lying on its left side: turn clockwise
				dx, dy = h-1-y, x
			case 7: // mirrored, lying on its right side
				dx, dy = h-1-y, w-1-x
			case 8: // lying on its right side: turn counter-clockwise
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], img.Pix[img.PixOffset(x, y):][:4])
		}
	}
	return dst
}
