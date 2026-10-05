package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/s3/s3test"
)

func TestS3PartPlanning(t *testing.T) {
	const MiB = 1 << 20
	for _, tc := range []struct {
		size, chunk, partSize int64
		count                 int
		last                  int64
	}{
		{0, 20 * MiB, 20 * MiB, 0, 0},
		{1, 20 * MiB, 20 * MiB, 1, 1},
		{50 * MiB, 20 * MiB, 20 * MiB, 3, 10 * MiB},
		{200000 * MiB, 20 * MiB, 20 * MiB, 10000, 20 * MiB},
		{300 << 30, 20 * MiB, 31 * MiB, 9910, (300 << 30) - 9909*31*MiB},
		{5 << 40, 20 * MiB, 525 * MiB, 9987, (5 << 40) - 9986*525*MiB},
	} {
		partSize := S3PartSize(tc.size, tc.chunk)
		count := S3PartCount(tc.size, partSize)
		var last int64
		if count > 0 {
			last = S3PartLen(count, tc.size, partSize)
		}
		if partSize != tc.partSize || count != tc.count || last != tc.last || count > s3.MaxParts {
			t.Errorf("%d bytes: parts of %d, %d of them, the last %d; want %d, %d, %d", tc.size, partSize, count, last, tc.partSize, tc.count, tc.last)
		}
	}
}

type s3Fixture struct {
	lib    *Library
	db     *db.DB
	fake   *s3test.Server
	bucket *s3.Bucket
	folder db.Folder
	now    time.Time
	purged []string
	logs   []string
}

func newS3Fixture(t *testing.T) *s3Fixture {
	t.Helper()
	ctx := context.Background()
	fake := s3test.New(t)
	fake.SetMinPartSize(1)
	b, err := s3.Open(fake.Config("lib/"), s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "share.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	folder, err := EnsureFirstFolder(ctx, d, "", "Share", t0)
	if err != nil {
		t.Fatal(err)
	}
	fx := &s3Fixture{db: d, fake: fake, bucket: b, folder: folder, now: t0}
	fake.SetClock(func() time.Time { return fx.now })
	rome, _ := time.LoadLocation("Europe/Rome")
	fx.lib = OpenS3Library(d, b, Layout{DataDir: dir}, rome, func() time.Time { return fx.now }, func(f string, a ...any) {
		fx.logs = append(fx.logs, f)
	})
	fx.lib.OnPurged = func(id string) { fx.purged = append(fx.purged, id) }
	return fx
}

// upload starts an upload of content in parts of partSize, as the server's S3 endpoint does,
// and sends the parts numbered in send, as a browser does.
func (fx *s3Fixture) upload(t *testing.T, name, content string, partSize int64, send ...int) db.File {
	t.Helper()
	ctx := context.Background()
	id := ids.New()
	f := db.File{ID: id, Name: name, Size: int64(len(content)), CreatedAt: fx.now, UpdatedAt: fx.now, FolderID: fx.folder.ID, S3PartSize: partSize}
	if f.Size > 0 {
		uploadID, err := fx.lib.StartS3Upload(ctx, id, name)
		if err != nil {
			t.Fatal(err)
		}
		f.S3UploadID = uploadID
	}
	if err := fx.db.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	for _, n := range send {
		start := int64(n-1) * partSize
		fx.sendPart(t, f, n, content[start:start+S3PartLen(n, f.Size, partSize)])
	}
	return f
}

func (fx *s3Fixture) sendPart(t *testing.T, f db.File, n int, data string) {
	t.Helper()
	link, _, err := fx.bucket.PartURL(context.Background(), fx.bucket.Key(f.ID), f.S3UploadID, n, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, link, strings.NewReader(data))
	res, err := fx.fake.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("part %d: %s", n, res.Status)
	}
}

func (fx *s3Fixture) file(t *testing.T, id string) db.File {
	t.Helper()
	f, err := fx.db.FileByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFinalizeS3(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	f := fx.upload(t, "IMG_1.jpg", "0123456789", 4, 1, 2)
	if err := fx.lib.Finalize(ctx, f.ID); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("with a part missing: %v", err)
	}
	fx.sendPart(t, f, 3, "89")
	if err := fx.lib.Finalize(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	got := fx.file(t, f.ID)
	if got.State != db.StateReady || got.RelPath != "2026-09-27/IMG_1.jpg" || got.Mime != "image/jpeg" || got.Kind != db.KindPhoto {
		t.Errorf("finalized: %+v", got)
	}
	if data, ok := fx.fake.Object("lib/files/" + f.ID); !ok || string(data) != "0123456789" {
		t.Errorf("object %q, %v", data, ok)
	}
	if ct, cd := fx.fake.ObjectHeaders("lib/files/" + f.ID); ct != "image/jpeg" || cd != ContentDisposition("IMG_1.jpg") {
		t.Errorf("stored with %q, %q", ct, cd)
	}
	if len(fx.fake.Uploads()) != 0 {
		t.Errorf("uploads left: %v", fx.fake.Uploads())
	}
	if err := fx.lib.Finalize(ctx, f.ID); err != nil {
		t.Errorf("finalizing twice: %v", err)
	}

	// A name that is taken gets a number, as on a drive.
	again := fx.upload(t, "img_1.JPG", "abc", 4, 1)
	if err := fx.lib.Finalize(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if got := fx.file(t, again.ID); got.RelPath != "2026-09-27/img_1 (2).JPG" {
		t.Errorf("a taken name: %q", got.RelPath)
	}

	file, err := fx.lib.Open(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(file); err != nil || string(rest) != "6789" {
		t.Errorf("after a seek %q, %v", rest, err)
	}
	file.Close()
	if err := fx.bucket.Remove(ctx, "lib/files/"+again.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.lib.Open(ctx, fx.file(t, again.ID)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening a file whose object is gone: %v", err)
	}
	if _, err := fx.lib.OpenFile(ctx, got); err == nil {
		t.Error("OpenFile in a bucket")
	}
}

func TestFinalizeS3RefusesPartsOfTheWrongSize(t *testing.T) {
	fx := newS3Fixture(t)
	f := fx.upload(t, "a.bin", "0123456789", 4, 1, 3)
	fx.sendPart(t, f, 2, "456") // one byte short
	if err := fx.lib.Finalize(context.Background(), f.ID); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("a part of 3 bytes instead of 4: %v", err)
	}
	fx.sendPart(t, f, 2, "4567") // sent again, now right
	if err := fx.lib.Finalize(context.Background(), f.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := fx.fake.Object("lib/files/" + f.ID); string(data) != "0123456789" {
		t.Errorf("object %q", data)
	}
}

func TestFinalizeS3AfterALostAnswer(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	f := fx.upload(t, "clip.mov", "0123456789", 5, 1, 2)
	// Complete went through, but its answer and the row's update didn't.
	parts, err := fx.bucket.Parts(ctx, fx.bucket.Key(f.ID), f.S3UploadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.bucket.Complete(ctx, fx.bucket.Key(f.ID), f.S3UploadID, []s3.Part{parts[1], parts[2]}); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Finalize(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if got := fx.file(t, f.ID); got.State != db.StateReady || got.Kind != db.KindVideo {
		t.Errorf("finalized: %+v", got)
	}

	// An upload the bucket dropped on its own.
	lost := fx.upload(t, "lost.jpg", "0123456789", 5, 1)
	if err := fx.bucket.Abort(ctx, fx.bucket.Key(lost.ID), lost.S3UploadID); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Finalize(ctx, lost.ID); !errors.Is(err, ErrUploadGone) {
		t.Errorf("an upload the bucket lost: %v", err)
	}
}

func TestFinalizeS3EmptyAndUnknownFiles(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	empty := fx.upload(t, "empty.txt", "", 4)
	if err := fx.lib.Finalize(ctx, empty.ID); err != nil {
		t.Fatal(err)
	}
	if data, ok := fx.fake.Object("lib/files/" + empty.ID); !ok || len(data) != 0 {
		t.Errorf("empty object %q, %v", data, ok)
	}
	if got := fx.file(t, empty.ID); got.State != db.StateReady || got.Mime != "text/plain; charset=utf-8" {
		t.Errorf("empty file: %+v", got)
	}

	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	noext := fx.upload(t, "scan", png, 16, 1, 2)
	if err := fx.lib.Finalize(ctx, noext.ID); err != nil {
		t.Fatal(err)
	}
	if got := fx.file(t, noext.ID); got.Mime != "image/png" || got.Kind != db.KindPhoto {
		t.Errorf("sniffed: %s, %s", got.Mime, got.Kind)
	}
}

func TestTerminateS3(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	f := fx.upload(t, "a.jpg", "0123456789", 4, 1)
	if err := fx.lib.Terminate(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.FileByID(ctx, f.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the row is still there: %v", err)
	}
	if len(fx.fake.Uploads()) != 0 {
		t.Errorf("the upload is still in the bucket: %v", fx.fake.Uploads())
	}
	if err := fx.lib.Terminate(ctx, f.ID); err != nil {
		t.Errorf("dropping it twice: %v", err)
	}
	done := fx.upload(t, "b.jpg", "abc", 4, 1)
	if err := fx.lib.Finalize(ctx, done.ID); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Terminate(ctx, done.ID); !errors.Is(err, ErrFinished) {
		t.Errorf("dropping a finished upload: %v", err)
	}
}

// Trash, restore, moves and folders change only rows in a bucket.
func TestS3LibraryChangesOnlyRows(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	a, b := fx.upload(t, "a.jpg", "aaa", 4, 1), fx.upload(t, "b.jpg", "bbb", 4, 1)
	for _, f := range []db.File{a, b} {
		if err := fx.lib.Finalize(ctx, f.ID); err != nil {
			t.Fatal(err)
		}
	}
	calls := len(fx.fake.Calls())
	other, err := fx.lib.CreateFolder(ctx, "Holidays", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := fx.lib.MoveFiles(ctx, []string{a.ID}, other.ID); err != nil || len(moved) != 1 {
		t.Fatalf("moved %v, %v", moved, err)
	}
	if got := fx.file(t, a.ID); got.FolderID != other.ID || got.MovedFrom != "" {
		t.Errorf("after the move: folder %s, moved from %q", got.FolderID, got.MovedFrom)
	}
	for _, name := range []string{"Summer", "Winter"} {
		renamed, err := fx.lib.RenameFolder(ctx, other.ID, name)
		if err != nil || renamed.RenamingFrom != nil {
			t.Fatalf("renaming to %s: %+v, %v", name, renamed, err)
		}
	}
	if _, err := fx.lib.Trash(ctx, []string{b.ID}, "admin"); err != nil {
		t.Fatal(err)
	}
	if restored, err := fx.lib.Restore(ctx, []string{b.ID}); err != nil || len(restored) != 1 {
		t.Fatalf("restored %v, %v", restored, err)
	}
	if got := fx.file(t, b.ID); got.State != db.StateReady || got.RelPath != "2026-09-27/b.jpg" {
		t.Errorf("restored: %+v", got)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Calls()[calls:]; slices.ContainsFunc(got, func(c string) bool { return !strings.HasPrefix(c, "ListMultipartUploads") }) {
		t.Errorf("calls to the bucket: %v", got)
	}

	// Deleting a folder trashes its files and drops its unfinished uploads.
	late := fx.upload(t, "late.jpg", "0123456789", 4, 1)
	if _, err := fx.db.ExecContext(ctx, "UPDATE files SET folder_id = ? WHERE id = ?", other.ID, late.ID); err != nil {
		t.Fatal(err)
	}
	trashed, err := fx.lib.DeleteFolder(ctx, other.ID, "admin")
	if err != nil || len(trashed) != 1 || trashed[0].ID != a.ID {
		t.Fatalf("trashed %v, %v", trashed, err)
	}
	if _, err := fx.db.FileByID(ctx, late.ID); !errors.Is(err, db.ErrNotFound) || len(fx.fake.Uploads()) != 0 {
		t.Errorf("the unfinished upload: %v, uploads %v", err, fx.fake.Uploads())
	}
	if _, ok := fx.fake.Object("lib/files/" + a.ID); !ok {
		t.Error("a trashed file's object is gone")
	}
}

func TestPurgeS3(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	var files []db.File
	for _, name := range []string{"a.jpg", "b.jpg"} {
		f := fx.upload(t, name, "abc", 4, 1)
		if err := fx.lib.Finalize(ctx, f.ID); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if _, err := fx.lib.Trash(ctx, []string{files[0].ID, files[1].ID}, "admin"); err != nil {
		t.Fatal(err)
	}
	if purged, err := fx.lib.Purge(ctx, []string{files[0].ID}); err != nil || len(purged) != 1 {
		t.Fatalf("purged %v, %v", purged, err)
	}
	if _, ok := fx.fake.Object("lib/files/" + files[0].ID); ok {
		t.Error("the object is still there")
	}
	// Without the bucket the row goes anyway; the object goes once it is back.
	fx.fake.Down(true)
	if purged, err := fx.lib.Purge(ctx, []string{files[1].ID}); err != nil || len(purged) != 1 {
		t.Fatalf("purged without the bucket: %v, %v", purged, err)
	}
	if _, err := fx.db.FileByID(ctx, files[1].ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the row is still there: %v", err)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	fx.fake.Down(false)
	if _, ok := fx.fake.Object("lib/files/" + files[1].ID); !ok {
		t.Fatal("the object went while the bucket was down")
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok := fx.fake.Object("lib/files/" + files[1].ID); ok {
		t.Error("the object is still there after the bucket came back")
	}
	if !slices.Equal(fx.purged, []string{files[0].ID, files[1].ID}) {
		t.Errorf("thumbnails removed for %v", fx.purged)
	}
}

func TestReconcileS3(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	ttl := 24 * time.Hour

	// Stuck halfway: the object is there, the row says finalizing.
	stuck := fx.upload(t, "stuck.jpg", "abc", 4, 1)
	parts, _ := fx.bucket.Parts(ctx, fx.bucket.Key(stuck.ID), stuck.S3UploadID)
	if err := fx.bucket.Complete(ctx, fx.bucket.Key(stuck.ID), stuck.S3UploadID, []s3.Part{parts[1]}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.MarkFinalizing(ctx, stuck.ID, "2026-09-27/stuck.jpg", "2026-09-27", fx.now); err != nil {
		t.Fatal(err)
	}
	complete := fx.upload(t, "complete.jpg", "abcdef", 4, 1, 2) // every part sent, never completed
	partial := fx.upload(t, "partial.jpg", "abcdef", 4, 1)
	young := fx.upload(t, "young.jpg", "abcdef", 4, 1)
	stray := fx.fake.StartUpload("lib/files/"+ids.New(), fx.now)
	theirs := fx.fake.StartUpload("other/files/x", fx.now)
	trashed := fx.upload(t, "trashed.jpg", "abc", 4, 1)
	if err := fx.lib.Finalize(ctx, trashed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.lib.Trash(ctx, []string{trashed.ID}, "admin"); err != nil {
		t.Fatal(err)
	}

	fx.now = fx.now.Add(ttl + time.Hour)
	if _, err := fx.db.ExecContext(ctx, "UPDATE files SET updated_at = ? WHERE id = ?", fx.now.UnixMilli(), young.ID); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Reconcile(ctx, ttl); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{stuck.ID, complete.ID} {
		if got := fx.file(t, id); got.State != db.StateReady {
			t.Errorf("%s: %s", got.Name, got.State)
		}
	}
	if _, err := fx.db.FileByID(ctx, partial.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the idle partial upload: %v", err)
	}
	if got := fx.file(t, young.ID); got.State != db.StateReceiving {
		t.Errorf("the young upload: %s", got.State)
	}
	if got := fx.file(t, trashed.ID); got.State != db.StateTrashed {
		t.Errorf("the trashed file: %s", got.State)
	}
	uploads := fx.fake.Uploads()
	if slices.Contains(uploads, stray) || slices.Contains(uploads, partial.S3UploadID) || !slices.Contains(uploads, young.S3UploadID) || !slices.Contains(uploads, theirs) {
		t.Errorf("uploads left: %v (stray %s, young %s, theirs %s)", uploads, stray, young.S3UploadID, theirs)
	}
}

func TestReconcileS3WithoutTheBucket(t *testing.T) {
	fx := newS3Fixture(t)
	ctx := context.Background()
	f := fx.upload(t, "a.jpg", "abc", 4, 1)
	fx.now = fx.now.Add(30 * 24 * time.Hour)
	fx.fake.Down(true)
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatalf("reconciling without the bucket: %v", err)
	}
	if got := fx.file(t, f.ID); got.State != db.StateReceiving {
		t.Errorf("dropped while the bucket was away: %s", got.State)
	}
	if !slices.ContainsFunc(fx.logs, func(l string) bool { return strings.Contains(l, "finishing") }) {
		t.Errorf("nothing logged: %q", fx.logs)
	}
}

func TestCheckS3(t *testing.T) {
	ctx := context.Background()
	fake := s3test.New(t)
	open := func(c *config.S3) *s3.Bucket {
		b, err := s3.Open(c, s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b := open(fake.Config("lib/"))
	origins := []string{"https://share.example.test", "http://192.168.8.52:8080"}
	check := func(b *s3.Bucket) []string {
		var codes []string
		for _, f := range CheckS3(ctx, t.TempDir(), b, origins).Problems {
			if strings.HasPrefix(f.Code, "s3_") { // the temporary data folder may be in memory
				codes = append(codes, f.Code)
			}
		}
		return codes
	}
	if got := check(b); len(got) != 0 {
		t.Errorf("a good bucket: %v", got)
	}
	fake.SetCORS(false)
	if got := check(b); !slices.Equal(got, []string{"s3_cors"}) {
		t.Errorf("without CORS rules: %v", got)
	}
	fake.SetCORS(true)
	fake.SetSkew(20 * time.Minute)
	if got := check(b); !slices.Equal(got, []string{"s3_clock_skew"}) {
		t.Errorf("a clock 20 minutes off: %v", got)
	}
	fake.SetSkew(0)
	wrong := fake.Config("lib/")
	wrong.AccessKeyID = "AKIDSOMEONEELSE0"
	if got := check(open(wrong)); !slices.Equal(got, []string{"s3_denied"}) {
		t.Errorf("unknown keys: %v", got)
	}
	fake.Down(true)
	if got := check(b); !slices.Equal(got, []string{"s3_unreachable"}) {
		t.Errorf("no network: %v", got)
	}
}
