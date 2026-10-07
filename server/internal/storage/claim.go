package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eschgi/share/server/internal/db"
)

// ClaimStorage makes sure a database keeps the storage its files are in: storage_dir on a
// drive, or the objects under a bucket's prefix. want is the config's StorageKey. A database
// without files may change; Share never moves files from one storage to another.
func ClaimStorage(ctx context.Context, d *db.DB, want string) error {
	have, err := d.Meta(ctx, "storage")
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	if have == want {
		return nil
	}
	files, err := d.HasFiles(ctx)
	if err != nil {
		return err
	}
	switch {
	case have == "" && files && want != "disk":
		// A database from before buckets: its files are on a drive.
		return fmt.Errorf("the files of this database are in storage_dir on a drive, but config.json names %s; Share doesn't move them into a bucket: give \"s3\" a new database", storageName(want))
	case have != "" && files:
		return fmt.Errorf("the files of this database are in %s, but config.json names %s; Share doesn't move them: put config.json back, or give the new storage a new database", storageName(have), storageName(want))
	case have != "":
		// Nothing was kept in the old storage, so nothing is left to remove there either.
		if err := d.DropS3Garbage(ctx); err != nil {
			return err
		}
	}
	return d.SetMeta(ctx, "storage", want)
}

// storageName says a StorageKey in words.
func storageName(key string) string {
	bucket, ok := strings.CutPrefix(key, "s3:")
	if !ok {
		return "storage_dir on a drive"
	}
	bucket, prefix, _ := strings.Cut(bucket, "/")
	if prefix == "" {
		return "the bucket " + bucket
	}
	return fmt.Sprintf("the bucket %s under %q", bucket, prefix)
}
