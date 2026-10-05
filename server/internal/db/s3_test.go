package db

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

func TestS3Uploads(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	folder := testFolder(t, d, "Family")
	if has, err := d.HasFiles(ctx); err != nil || has {
		t.Fatalf("HasFiles of a new database = %v, %v", has, err)
	}
	f := File{ID: ids.New(), Name: "clip.mp4", Size: 50 << 20, CreatedAt: t0, UpdatedAt: t0, FolderID: folder,
		S3UploadID: "VXBsb2FkIElE", S3PartSize: 20 << 20}
	if err := d.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, err := d.FileByID(ctx, f.ID)
	if err != nil || got.S3UploadID != "VXBsb2FkIElE" || got.S3PartSize != 20<<20 {
		t.Fatalf("read back %+v, %v", got, err)
	}
	disk := File{ID: ids.New(), Name: "a.jpg", Size: 1, CreatedAt: t0, UpdatedAt: t0, FolderID: folder}
	if err := d.InsertReceiving(ctx, disk); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.FileByID(ctx, disk.ID); got.S3UploadID != "" || got.S3PartSize != 0 {
		t.Errorf("a tus upload has %q, %d", got.S3UploadID, got.S3PartSize)
	}
	if has, _ := d.HasFiles(ctx); !has {
		t.Error("HasFiles misses arriving files")
	}
}

func TestPurgeToS3Garbage(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	folder := testFolder(t, d, "Family")
	ready := func(name string) string {
		id := ids.New()
		if err := d.InsertReceiving(ctx, File{ID: id, Name: name, Size: 3, CreatedAt: t0, UpdatedAt: t0, FolderID: folder}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.MarkFinalizing(ctx, id, "2026-09-27/"+name, "2026-09-27", t0); err != nil {
			t.Fatal(err)
		}
		if err := d.MarkReady(ctx, id, "image/jpeg", KindPhoto, t0); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := ready("a.jpg"), ready("b.jpg")
	if _, err := d.TrashFiles(ctx, []string{a}, "admin", t0); err != nil {
		t.Fatal(err)
	}
	if purged, err := d.PurgeToS3Garbage(ctx, b, t0, "files/"+b); err != nil || purged {
		t.Fatalf("purging a file that isn't in the trash: %v, %v", purged, err)
	}
	if purged, err := d.PurgeToS3Garbage(ctx, a, t0, "files/"+a, "thumbs/"+a+".jpg"); err != nil || !purged {
		t.Fatalf("purging a trashed file: %v, %v", purged, err)
	}
	if _, err := d.FileByID(ctx, a); err != ErrNotFound {
		t.Errorf("the row is still there: %v", err)
	}
	if err := d.ForgetS3Garbage(ctx, "files/never"); err != nil {
		t.Fatal(err)
	}
	keys, err := d.S3Garbage(ctx, 10)
	if err != nil || !slices.Equal(keys, []string{"files/" + a, "thumbs/" + a + ".jpg"}) {
		t.Fatalf("garbage %v, %v", keys, err)
	}
	for _, key := range keys {
		if err := d.ForgetS3Garbage(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	if keys, _ := d.S3Garbage(ctx, 10); len(keys) != 0 {
		t.Errorf("garbage after forgetting: %v", keys)
	}
	for i := range 3 {
		if _, err := d.ExecContext(ctx, "INSERT INTO s3_garbage (key, created_at) VALUES (?, ?)", "k"+string(rune('a'+i)), ms(t0.Add(time.Duration(-i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if keys, _ := d.S3Garbage(ctx, 2); !slices.Equal(keys, []string{"kc", "kb"}) {
		t.Errorf("oldest first: %v", keys)
	}
	if err := d.DropS3Garbage(ctx); err != nil {
		t.Fatal(err)
	}
	if keys, _ := d.S3Garbage(ctx, 10); len(keys) != 0 {
		t.Errorf("garbage after dropping: %v", keys)
	}
}
