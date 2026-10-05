package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/dbtest"
	"github.com/eschgi/share/server/internal/ids"
)

func TestClaimStorage(t *testing.T) {
	ctx := context.Background()
	open := func(t *testing.T) *db.DB { return dbtest.Open(t, t.TempDir()) }
	addFile := func(t *testing.T, d *db.DB) {
		folder, err := EnsureFirstFolder(ctx, d, t.TempDir(), "Share", t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.InsertReceiving(ctx, db.File{ID: ids.New(), Name: "a.jpg", Size: 1, CreatedAt: t0, UpdatedAt: t0, FolderID: folder.ID}); err != nil {
			t.Fatal(err)
		}
	}
	stored := func(d *db.DB) string { v, _ := d.Meta(ctx, "storage"); return v }

	t.Run("a library from before buckets stays on its drive", func(t *testing.T) {
		d := open(t)
		addFile(t, d)
		if err := ClaimStorage(ctx, d, "s3:family/share/"); err == nil || !strings.Contains(err.Error(), "new data_dir") {
			t.Errorf("moving it to a bucket: %v", err)
		}
		if err := ClaimStorage(ctx, d, "disk"); err != nil || stored(d) != "disk" {
			t.Errorf("keeping the drive: %v, %q", err, stored(d))
		}
	})
	t.Run("a bucket keeps its library", func(t *testing.T) {
		d := open(t)
		if err := ClaimStorage(ctx, d, "s3:family/share/"); err != nil || stored(d) != "s3:family/share/" {
			t.Fatalf("a new database: %v, %q", err, stored(d))
		}
		addFile(t, d)
		for _, other := range []string{"disk", "s3:family/other/", "s3:friends/share/"} {
			if err := ClaimStorage(ctx, d, other); err == nil || !strings.Contains(err.Error(), `the bucket family under "share/"`) {
				t.Errorf("%s: %v", other, err)
			}
		}
		if err := ClaimStorage(ctx, d, "s3:family/share/"); err != nil {
			t.Errorf("the same bucket: %v", err)
		}
	})
	t.Run("an empty library may change", func(t *testing.T) {
		d := open(t)
		if err := ClaimStorage(ctx, d, "disk"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.ExecContext(ctx, "INSERT INTO s3_garbage (key, created_at) VALUES ('files/x', 0)"); err != nil {
			t.Fatal(err)
		}
		if err := ClaimStorage(ctx, d, "s3:family/"); err != nil || stored(d) != "s3:family/" {
			t.Errorf("to a bucket: %v, %q", err, stored(d))
		}
		if keys, _ := d.S3Garbage(ctx, 10); len(keys) != 0 {
			t.Errorf("the old storage's garbage is kept: %v", keys)
		}
	})
}
