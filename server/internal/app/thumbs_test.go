package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
)

// picture is w×h pixels: red on the left half, blue on the right.
func picture(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{200, 0, 0, 255}
			if x >= w/2 {
				c = color.NRGBA{0, 0, 200, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// jpegBytes encodes picture(w, h). An orientation above 1 goes into an EXIF segment, the way
// a phone marks a photo it stored lying on its side.
func jpegBytes(t *testing.T, w, h int, orientation uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, picture(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if orientation <= 1 {
		return b.Bytes()
	}
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(tiff[18:], orientation)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}
	return append(append(out, app1...), b.Bytes()[2:]...)
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, picture(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// sendFile uploads data in one piece and returns the file id.
func (c tus) sendFile(name string, data []byte) string {
	c.e.t.Helper()
	loc := c.mustCreate(name, len(data))
	c.send(loc, data, 0, max(len(data), 1))
	return idOf(loc)
}

func (e *env) putThumb(token, id, query, contentType string, body []byte) response {
	e.t.Helper()
	return e.do(nil, "PUT", "/api/files/"+id+"/thumb"+query, token, bytes.NewReader(body),
		map[string]string{"Content-Type": contentType})
}

func TestUploaderSendsTheThumbnail(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	token, _ := e.unlockApp(pin.Code, "")
	c := tus{e, token}
	id := c.sendFile("IMG_1.jpg", jpegBytes(t, 64, 48, 1))
	thumb := jpegBytes(t, 32, 24, 1)

	fixture := readFixture(t, "api/thumb_put.json")
	r := e.putThumb(token, id, "?width=4032&height=3024&duration_ms=0", "image/jpeg", thumb)
	if r.status != int(fixture["status"].(float64)) {
		t.Fatalf("PUT: %d %s", r.status, r.body)
	}
	if stored, err := os.ReadFile(e.app.Thumbs.Path(id)); err != nil || !bytes.Equal(stored, thumb) {
		t.Fatalf("stored thumbnail: %v", err)
	}
	if f := e.file(id); f.Thumb != db.ThumbClient || *f.Width != 4032 || *f.Height != 3024 || *f.DurationMS != 0 {
		t.Fatalf("row: %+v", f)
	}

	other, _ := e.unlockApp(pin.Code, "")
	unfinished := idOf(c.mustCreate("IMG_2.jpg", 10))
	for _, tc := range []struct {
		name, token, id, query, contentType string
		body                                []byte
		status                              int
		code                                string
	}{
		{"another session", other, id, "", "image/jpeg", thumb, 404, "not_found"},
		{"an unfinished upload", token, unfinished, "", "image/jpeg", thumb, 404, "not_found"},
		{"no such file", token, "aaaaaaaaaaaaaaaaaaaaaaaaaa", "", "image/jpeg", thumb, 404, "not_found"},
		{"not a JPEG", token, id, "", "image/png", pngBytes(t, 8, 8), 415, "unsupported_media_type"},
		{"not a picture", token, id, "", "image/jpeg", []byte("hello"), 400, "bad_thumbnail"},
		{"too wide", token, id, "", "image/jpeg", jpegBytes(t, 2000, 10, 1), 400, "bad_thumbnail"},
		{"too big", token, id, "", "image/jpeg", make([]byte, 512<<10+1), 413, "too_large"},
		{"a bad width", token, id, "?width=-1", "image/jpeg", thumb, 400, "bad_request"},
		{"no session", "", id, "", "image/jpeg", thumb, 401, "unauthorized"},
	} {
		if r := e.putThumb(tc.token, tc.id, tc.query, tc.contentType, tc.body); r.status != tc.status || r.errorCode() != tc.code {
			t.Errorf("%s: %d %s, want %d %s", tc.name, r.status, r.body, tc.status, tc.code)
		}
	}

	e.clock.Add(25 * time.Hour)
	if r := e.putThumb(token, id, "", "image/jpeg", thumb); r.status != http.StatusForbidden || r.errorCode() != "forbidden" {
		t.Errorf("a day later: %d %s, want 403 forbidden", r.status, r.body)
	}
	// The server leaves photos alone that have a thumbnail from their uploader.
	if n, err := e.app.Thumbs.MakePending(context.Background()); err != nil || n != 0 {
		t.Errorf("MakePending: %d, %v; want nothing to do", n, err)
	}
}

func TestServerMakesThumbnailsForPhotosWithout(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	token, _ := e.unlockApp(pin.Code, "")
	c := tus{e, token}
	turned := c.sendFile("IMG_2.jpg", jpegBytes(t, 600, 400, 6)) // stored lying on its side
	drawing := c.sendFile("drawing.png", pngBytes(t, 300, 200))
	broken := c.sendFile("broken.jpg", jpegBytes(t, 600, 400, 1)[:300])
	video := c.sendFile("clip.mp4", randomBytes(t, 1000))

	ctx := context.Background()
	if n, err := e.app.Thumbs.MakePending(ctx); err != nil || n != 0 {
		t.Fatalf("right after the upload: %d, %v; the uploader gets 2 minutes first", n, err)
	}
	e.clock.Add(2 * time.Minute)
	if n, err := e.app.Thumbs.MakePending(ctx); err != nil || n != 3 {
		t.Fatalf("MakePending: %d, %v; want the 3 photos", n, err)
	}

	for _, tc := range []struct {
		id                   string
		width, height        int64 // of the original, upright
		thumbW, thumbH       int
		topColour, botColour color.RGBA
	}{
		// Turned upright: the left half, red, is now on top.
		{turned, 400, 600, 341, 512, color.RGBA{200, 0, 0, 255}, color.RGBA{0, 0, 200, 255}},
		{drawing, 300, 200, 300, 200, color.RGBA{200, 0, 0, 255}, color.RGBA{200, 0, 0, 255}},
	} {
		f := e.file(tc.id)
		if f.Thumb != db.ThumbServer || f.Width == nil || *f.Width != tc.width || *f.Height != tc.height {
			t.Errorf("%s: thumb %s, %v×%v", f.Name, f.Thumb, f.Width, f.Height)
			continue
		}
		data, err := os.ReadFile(e.app.Thumbs.Path(tc.id))
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if b := img.Bounds(); b.Dx() != tc.thumbW || b.Dy() != tc.thumbH {
			t.Errorf("%s: thumbnail %d×%d, want %d×%d", f.Name, b.Dx(), b.Dy(), tc.thumbW, tc.thumbH)
		}
		top := color.RGBAModel.Convert(img.At(tc.thumbW/4, tc.thumbH/10)).(color.RGBA)
		bottom := color.RGBAModel.Convert(img.At(tc.thumbW/4, tc.thumbH*9/10)).(color.RGBA)
		if !near(top, tc.topColour) || !near(bottom, tc.botColour) {
			t.Errorf("%s: top %v, bottom %v; want %v, %v", f.Name, top, bottom, tc.topColour, tc.botColour)
		}
	}
	if f := e.file(broken); f.Thumb != db.ThumbFailed {
		t.Errorf("broken photo: thumb %s, want failed", f.Thumb)
	}
	if f := e.file(video); f.Thumb != db.ThumbNone {
		t.Errorf("video: thumb %s; only its uploader can make one", f.Thumb)
	}
	if n, err := e.app.Thumbs.MakePending(ctx); err != nil || n != 0 {
		t.Errorf("second pass: %d, %v; want nothing left", n, err)
	}

	// A thumbnail from the uploader still replaces the server's own.
	thumb := jpegBytes(t, 20, 30, 1)
	if r := e.putThumb(token, turned, "", "image/jpeg", thumb); r.status != http.StatusNoContent {
		t.Fatalf("PUT after the server's: %d %s", r.status, r.body)
	}
	if f := e.file(turned); f.Thumb != db.ThumbClient || *f.Width != 400 {
		t.Errorf("after the uploader's: thumb %s, width %v", f.Thumb, *f.Width)
	}
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) bool { return x+24 >= y && y+24 >= x }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

func TestServerThumbnailsSurviveBadPhotos(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	token, _ := e.unlockApp(pin.Code, "")
	c := tus{e, token}
	// A progressive 36 MP header with nothing behind it: decoding it would need over 500 MB,
	// so the server must refuse before it starts.
	sof := []byte{0xFF, 0xD8, 0xFF, 0xC2, 0, 17, 8, 0x17, 0x70, 0x17, 0x70, 3, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0}
	huge := c.sendFile("huge.jpg", append(sof, 0xFF, 0xDA, 0, 8, 1, 1, 0, 0, 0x3F, 0, 0xFF, 0xD9))
	e.clock.Add(time.Minute)
	locked := c.sendFile("locked.jpg", jpegBytes(t, 60, 40, 1))
	e.clock.Add(time.Minute)
	fine := c.sendFile("fine.jpg", jpegBytes(t, 60, 40, 1))
	e.clock.Add(2 * time.Minute)

	ctx := context.Background()
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their permissions")
	}
	lockedPath := e.disk(e.file(locked))
	if err := os.Chmod(lockedPath, 0); err != nil {
		t.Fatal(err)
	}
	// An unreadable photo is tried again on the next runs, holding up the ones after it...
	for try := 1; try < 3; try++ {
		if _, err := e.app.Thumbs.MakePending(ctx); err == nil {
			t.Fatalf("run %d: no error for the unreadable photo", try)
		}
		if f := e.file(locked); f.Thumb != db.ThumbNone {
			t.Fatalf("run %d: unreadable photo is %s, want it back as none", try, f.Thumb)
		}
	}
	if f := e.file(huge); f.Thumb != db.ThumbFailed {
		t.Errorf("huge progressive JPEG: thumb %s, want failed without decoding", f.Thumb)
	}
	// ...but only a few times; then it is given up and the rest go on.
	if _, err := e.app.Thumbs.MakePending(ctx); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if f := e.file(locked); f.Thumb != db.ThumbFailed {
		t.Errorf("after three tries: thumb %s, want failed", f.Thumb)
	}
	if f := e.file(fine); f.Thumb != db.ThumbServer {
		t.Errorf("the photo after it: thumb %s, want server", f.Thumb)
	}
}
