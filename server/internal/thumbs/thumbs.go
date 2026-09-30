// Package thumbs keeps the small previews the app shows in the library. The browser or
// phone that sent a file makes one right after the upload, which also works for videos. For
// photos that arrive without one, the server makes its own a little later.
package thumbs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // registered for image.Decode
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

const (
	// MaxBytes is the largest thumbnail an uploader may send.
	MaxBytes = 512 << 10
	// maxSentSide limits the width and height of a sent thumbnail.
	maxSentSide = 1024
	// uploaderWindow is how long after the upload its sender may still send a thumbnail.
	uploaderWindow = 24 * time.Hour

	// The server's own thumbnails.
	side    = 512
	quality = 80
	// delay gives the uploader time to send a thumbnail before the server makes one.
	delay = 2 * time.Minute
	// maxDecodeBytes is the most memory decoding one photo may take; the router has little.
	maxDecodeBytes = 128 << 20
	// maxPixels keeps the decoding time reasonable on a router's processor.
	maxPixels = 40_000_000
	// tries is how often a photo is tried again after a read error before it is given up.
	tries = 3
	batch = 20
)

var (
	// ErrNotFound means there is no such file in the library, or it isn't the caller's.
	ErrNotFound = errors.New("no such file")
	// ErrTooLate means the upload is more than a day old.
	ErrTooLate = errors.New("thumbnails can only be sent within a day of the upload")
	// ErrInvalid means the thumbnail isn't a JPEG of at most 1024×1024 pixels.
	ErrInvalid = errors.New("the thumbnail must be a JPEG of at most 1024×1024 pixels")
)

// sentDecodes limits how many sent thumbnails are checked at once; each decode takes up to
// about 15 MB.
var sentDecodes = make(chan struct{}, 2)

// Meta is what the uploader measured on the original file. Nil fields are unknown.
type Meta struct {
	Width, Height, DurationMS *int64
}

// Store saves thumbnails as <Dir>/ab/<id>.jpg and records them in the database.
type Store struct {
	DB   *db.DB
	Dir  string
	Root *os.Root // the storage folder, for reading library files
	Now  func() time.Time
	Logf func(string, ...any)

	mu     sync.Mutex     // one writer at a time, so a sent and a made thumbnail never cross
	failed map[string]int // read errors per photo, for giving up after a few
}

// Path is where the thumbnail of file id is kept.
func (s *Store) Path(id string) string { return filepath.Join(s.Dir, id[:2], id+".jpg") }

// CanSend checks that p may send a thumbnail for file id: its uploader within a day of the
// upload, or an admin. The API asks before it reads the picture.
func (s *Store) CanSend(ctx context.Context, p *auth.Principal, id string) error {
	if !ids.Valid(id) {
		return ErrNotFound
	}
	f, err := s.DB.FileByID(ctx, id)
	if errors.Is(err, db.ErrNotFound) || (err == nil && f.State != db.StateReady) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if p.Kind == auth.KindDevice && p.Role == "admin" {
		return nil
	}
	if !p.Owns(f) {
		return ErrNotFound
	}
	if f.UploadedAt == nil || s.Now().Sub(*f.UploadedAt) > uploaderWindow {
		return ErrTooLate
	}
	return nil
}

// PutSent stores a thumbnail sent by the file's uploader, or by an admin. It replaces any
// thumbnail the file already has.
func (s *Store) PutSent(ctx context.Context, p *auth.Principal, id string, data []byte, m Meta) error {
	if err := s.CanSend(ctx, p, id); err != nil {
		return err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxSentSide || cfg.Height > maxSentSide {
		return ErrInvalid
	}
	select {
	case sentDecodes <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err = jpeg.Decode(bytes.NewReader(data))
	<-sentDecodes
	if err != nil {
		return ErrInvalid // the app shows these; only whole, valid pictures get through
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(id, data); err != nil {
		return err
	}
	ok, err := s.DB.SetClientThumb(ctx, id, m.Width, m.Height, m.DurationMS, s.Now())
	if err != nil {
		return err
	}
	if !ok { // it left the library in the meantime
		os.Remove(s.Path(id))
		return ErrNotFound
	}
	return nil
}

// Run makes the server's thumbnails once a minute until ctx ends.
func (s *Store) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if _, err := s.MakePending(ctx); err != nil && ctx.Err() == nil {
			s.Logf("thumbs: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// MakePending makes thumbnails for photos that have waited long enough for one from their
// uploader, and returns how many it looked at. Each photo is claimed before it is decoded
// and stays marked as failed unless that works, so a photo that crashes the server isn't
// tried again after the restart. After a read error it goes back, and the run stops, since
// the drive may be gone for a moment; after a few such tries it is given up.
func (s *Store) MakePending(ctx context.Context) (int, error) {
	files, err := s.DB.ThumbCandidates(ctx, s.Now().Add(-delay), batch)
	if err != nil {
		return 0, err
	}
	if len(files) > 0 {
		defer debug.FreeOSMemory() // hand a big decoded photo's memory back to the router
	}
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if ok, err := s.DB.ClaimThumb(ctx, f.ID, s.Now()); err != nil || !ok {
			if err != nil {
				return i, err
			}
			continue // its uploader's thumbnail came in just now
		}
		err := s.make(ctx, f)
		var u unusable
		switch {
		case err == nil:
			s.forget(f.ID)
		case errors.As(err, &u):
			s.forget(f.ID)
			s.Logf("thumbs: no thumbnail for %s: %v", f.RelPath, err)
		case s.retry(f.ID):
			if rerr := s.DB.ReleaseThumb(ctx, f.ID, s.Now()); rerr != nil {
				return i, rerr
			}
			return i, fmt.Errorf("%s: %w", f.RelPath, err)
		default:
			s.Logf("thumbs: giving up on %s: %v", f.RelPath, err)
		}
	}
	return len(files), nil
}

// retry counts a read error for photo id and reports whether to try it again later.
func (s *Store) retry(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = map[string]int{}
	}
	s.failed[id]++
	if s.failed[id] < tries {
		return true
	}
	delete(s.failed, id)
	return false
}

// Remove deletes a file's thumbnail, when the file is gone for good.
func (s *Store) Remove(id string) {
	if err := os.Remove(s.Path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.Logf("thumbs: removing %s: %v", id, err)
	}
	s.forget(id)
}

func (s *Store) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failed, id)
}

// unusable wraps the reasons a photo can't get a thumbnail from the server.
type unusable struct{ error }

func (u unusable) Unwrap() error { return u.error }

// readErrors remembers the first read error of r, so a failed decode can tell a broken
// picture from a drive that didn't answer.
type readErrors struct {
	r   io.Reader
	err error
}

func (e *readErrors) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// decodeFailed classifies a decoder's error: a read error is worth another try, anything
// else means the picture is broken.
func decodeFailed(err error, r *readErrors) error {
	if r.err != nil {
		return r.err
	}
	return unusable{err}
}

func (s *Store) make(ctx context.Context, f db.File) error {
	switch f.Mime {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return unusable{fmt.Errorf("can't read %s", f.Mime)}
	}
	file, err := s.Root.Open(f.RelPath)
	if errors.Is(err, fs.ErrNotExist) {
		return unusable{err}
	}
	if err != nil {
		return err
	}
	defer file.Close()
	src := &readErrors{r: file}

	cfg, format, err := image.DecodeConfig(bufio.NewReader(src))
	if err != nil {
		return decodeFailed(err, src)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return unusable{fmt.Errorf("%d×%d pixels is too large", cfg.Width, cfg.Height)}
	}
	orientation := 1
	need := 2 * int64(cfg.Width) * int64(cfg.Height) * pixelBytes(cfg.ColorModel) // PNG may interlace, GIF composes
	if format == "jpeg" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		h, err := readJPEGHeader(src)
		if err != nil {
			return decodeFailed(err, src)
		}
		orientation, need = h.orientation, h.decodeBytes()
	}
	if need > maxDecodeBytes {
		return unusable{fmt.Errorf("decoding %d×%d pixels would take %d MB", cfg.Width, cfg.Height, need>>20)}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	img, _, err := image.Decode(bufio.NewReaderSize(src, 64<<10))
	if err != nil {
		return decodeFailed(err, src)
	}
	w, h := fit(img.Bounds().Dx(), img.Bounds().Dy(), side)
	small := orient(shrink(img, w, h), orientation)
	img = nil // let the big picture go before encoding

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, small, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	width, height := int64(cfg.Width), int64(cfg.Height)
	if orientation >= 5 {
		width, height = height, width
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// The uploader's thumbnail may have come in while this one was being made; it wins.
	cur, err := s.DB.FileByID(ctx, f.ID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if cur.Thumb != db.ThumbFailed || cur.State != db.StateReady {
		return nil
	}
	if err := s.write(f.ID, buf.Bytes()); err != nil {
		return err
	}
	_, err = s.DB.SetServerThumb(ctx, f.ID, width, height, s.Now())
	return err
}

// pixelBytes is how many bytes per pixel Go's PNG and GIF decoders use for a colour model.
func pixelBytes(m color.Model) int64 {
	switch m {
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	case color.Gray16Model:
		return 2
	case color.GrayModel, color.AlphaModel:
		return 1
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 4
}

// write saves a thumbnail through a temporary file, so a reader never sees half of one.
func (s *Store) write(id string, data []byte) error {
	dir := filepath.Dir(s.Path(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, id+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.Path(id))
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
