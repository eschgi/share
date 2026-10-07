package thumbs

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"testing"
)

// withOrientation returns a JPEG of img carrying an EXIF orientation tag, written in the
// given byte order the way phones do.
func withOrientation(t *testing.T, img image.Image, o uint16, bo binary.ByteOrder) []byte {
	t.Helper()
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 8+2+12+4)
	if bo == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8) // the first directory follows the header
	bo.PutUint16(tiff[8:], 1) // one entry
	bo.PutUint16(tiff[10:], 0x0112)
	bo.PutUint16(tiff[12:], 3) // SHORT
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], o)
	app1 := append([]byte("Exif\x00\x00"), tiff...)

	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(out[4:], uint16(len(app1)+2))
	out = append(out, app1...)
	return append(out, enc.Bytes()[2:]...) // the encoded image without its own SOI
}

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestJPEGOrientation(t *testing.T) {
	img := solid(8, 8, color.White)
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"little endian", withOrientation(t, img, 6, binary.LittleEndian), 6},
		{"big endian", withOrientation(t, img, 8, binary.BigEndian), 8},
		{"out of range", withOrientation(t, img, 9, binary.BigEndian), 1},
		{"no EXIF", func() []byte { var b bytes.Buffer; jpeg.Encode(&b, img, nil); return b.Bytes() }(), 1},
		{"not a JPEG", []byte("\x89PNG\r\n\x1a\n"), 1},
		{"cut off", withOrientation(t, img, 6, binary.LittleEndian)[:12], 1},
	} {
		if got := jpegOrientation(bytes.NewReader(tc.data)); got != tc.want {
			t.Errorf("%s: orientation %d, want %d", tc.name, got, tc.want)
		}
	}
	// The tagged file still decodes: the segment is well-formed.
	if _, err := jpeg.Decode(bytes.NewReader(withOrientation(t, img, 6, binary.LittleEndian))); err != nil {
		t.Fatal(err)
	}
}

func TestFit(t *testing.T) {
	for _, tc := range []struct{ w, h, ww, wh int }{
		{4032, 3024, 512, 384},
		{3024, 4032, 384, 512},
		{300, 200, 300, 200},
		{100000, 10, 512, 1},
	} {
		if w, h := fit(tc.w, tc.h, 512); w != tc.ww || h != tc.wh {
			t.Errorf("fit(%d, %d) = %d×%d, want %d×%d", tc.w, tc.h, w, h, tc.ww, tc.wh)
		}
	}
}

func TestShrinkAverages(t *testing.T) {
	src := solid(4, 4, color.Black)
	for y := range 2 {
		for x := range 2 {
			src.Set(x, y, color.White) // the top-left quarter
		}
	}
	src.Set(3, 3, color.RGBA{255, 0, 0, 255}) // one red pixel in the bottom-right quarter
	got := shrink(src, 2, 2)
	want := []color.RGBA{{255, 255, 255, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}, {63, 0, 0, 255}}
	for i, w := range want {
		if c := got.RGBAAt(i%2, i/2); c != w {
			t.Errorf("pixel %d = %v, want %v", i, c, w)
		}
	}
}

func TestShrinkJPEG(t *testing.T) {
	var b bytes.Buffer
	jpeg.Encode(&b, solid(64, 48, color.RGBA{200, 100, 50, 255}), &jpeg.Options{Quality: 95})
	img, err := jpeg.Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.YCbCr); !ok {
		t.Fatalf("decoded as %T; the test wants the YCbCr path", img)
	}
	got := shrink(img, 16, 12).RGBAAt(8, 6)
	near := func(a, b uint8) bool { return a+4 >= b && b+4 >= a }
	if !near(got.R, 200) || !near(got.G, 100) || !near(got.B, 50) {
		t.Errorf("colour %v, want about {200 100 50}", got)
	}
}

func TestOrient(t *testing.T) {
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 1)) // red on the left, blue on the right
	src.SetRGBA(0, 0, red)
	src.SetRGBA(1, 0, blue)
	for _, tc := range []struct {
		o          int
		w, h       int
		first, end color.RGBA // pixel (0,0) and the last pixel
	}{
		{1, 2, 1, red, blue},
		{2, 2, 1, blue, red},
		{3, 2, 1, blue, red},
		{4, 2, 1, red, blue},
		{5, 1, 2, red, blue},
		{6, 1, 2, red, blue},
		{7, 1, 2, blue, red},
		{8, 1, 2, blue, red},
	} {
		got := orient(src, tc.o)
		b := got.Bounds()
		if b.Dx() != tc.w || b.Dy() != tc.h {
			t.Errorf("orientation %d: %d×%d, want %d×%d", tc.o, b.Dx(), b.Dy(), tc.w, tc.h)
			continue
		}
		if f, e := got.RGBAAt(0, 0), got.RGBAAt(tc.w-1, tc.h-1); f != tc.first || e != tc.end {
			t.Errorf("orientation %d: first %v last %v, want %v %v", tc.o, f, e, tc.first, tc.end)
		}
	}
}

// frameOnly is the start of a JPEG up to its image data: a frame header and nothing to decode.
func frameOnly(progressive bool, w, h int, sampling ...byte) []byte {
	sof := byte(0xC0)
	if progressive {
		sof = 0xC2
	}
	seg := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(len(sampling))}
	for i, s := range sampling {
		seg = append(seg, byte(i+1), s, 0)
	}
	out := []byte{0xFF, 0xD8, 0xFF, sof, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	out = append(out, seg...)
	return append(out, 0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0xFF, 0xD9)
}

func TestDecodeBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want int64
	}{
		// 4:2:0 as phones write it: 1.5 bytes per pixel.
		{"baseline 12 MP", frameOnly(false, 4000, 3000, 0x22, 0x11, 0x11), 18_048_000},
		// Progressive keeps 256 bytes per block and component on top: 7.5 per pixel.
		{"progressive 12 MP", frameOnly(true, 4000, 3000, 0x22, 0x11, 0x11), 90_240_000},
		{"progressive 36 MP, 4:4:4", frameOnly(true, 6000, 6000, 0x11, 0x11, 0x11), 540_000_000},
		{"grey", frameOnly(false, 800, 600, 0x11), 480_000},
	} {
		h, err := readJPEGHeader(bytes.NewReader(tc.data))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := h.decodeBytes(); got != tc.want {
			t.Errorf("%s: %d bytes, want %d", tc.name, got, tc.want)
		}
	}
	// Go's own encoder writes 4:2:0.
	var b bytes.Buffer
	jpeg.Encode(&b, solid(64, 48, color.White), nil)
	h, err := readJPEGHeader(&b)
	if err != nil || h.width != 64 || h.height != 48 || len(h.comps) != 3 || h.comps[0] != (jpegComp{2, 2}) || h.progressive {
		t.Errorf("Go-encoded JPEG: %+v, %v", h, err)
	}
	if _, err := readJPEGHeader(bytes.NewReader(append(frameOnly(false, 8, 8, 0x11)[:2], 0xFF, 0xC3, 0, 2))); err == nil {
		t.Error("a lossless JPEG passed; Go can't decode those")
	}
}

// Phones' full-size photos get a thumbnail from the server too: 50 MP from a Pixel's HI RES
// mode, 48 MP from an iPhone, 64 MP from others (issue #2).
func TestPhonePhotosAreNotTooLarge(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{8160, 6144}, {8064, 6048}, {9248, 6936}} {
		h, err := readJPEGHeader(bytes.NewReader(frameOnly(false, tc.w, tc.h, 0x22, 0x11, 0x11)))
		if err != nil {
			t.Fatal(err)
		}
		if int64(tc.w)*int64(tc.h) > maxPixels || h.decodeBytes() > maxDecodeBytes {
			t.Errorf("%d×%d, %d MB to decode: too large for a thumbnail", tc.w, tc.h, h.decodeBytes()>>20)
		}
	}
}

// A thumbnail's folder is named after the id's last two characters: an id starts with the time
// it was made, so its first ones would put years of thumbnails into one folder.
func TestPathUsesTheRandomEndOfTheID(t *testing.T) {
	s := &Store{Dir: filepath.Join("data", "thumbs")}
	id := "0199b3a4-6f2e-7c41-9d3a-5e8f0b2c4d6e"
	if got, want := s.Path(id), filepath.Join("data", "thumbs", "6e", id+".jpg"); got != want {
		t.Errorf("Path = %s, want %s", got, want)
	}
}
