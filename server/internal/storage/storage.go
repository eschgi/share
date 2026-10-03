// Package storage owns the files on disk: the storage folder and its checks, the library
// layout, and moving finished uploads into the library.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// MarkerName is the file `share init` puts into the storage folder. The server waits for it
// instead of creating the folder itself: if the drive or volume isn't mounted yet, creating the
// folder would quietly put every upload on the system's own disk, or inside a container.
const MarkerName = ".share-storage"

// Layout names the folders.
type Layout struct {
	StorageDir string // the library: a directory per folder, with the day folders inside
	DataDir    string // database, thumbnails, certificates
}

// UploadsDir holds unfinished tus uploads. It sits inside the storage folder, on the same
// drive, so finishing an upload is a rename.
func (l Layout) UploadsDir() string { return filepath.Join(l.StorageDir, ".uploads") }

// TrashDir holds deleted files for a while; the same drive again, so deleting is a rename.
func (l Layout) TrashDir() string { return filepath.Join(l.StorageDir, ".trash") }

// DBPath is the SQLite database.
func (l Layout) DBPath() string { return filepath.Join(l.DataDir, "share.db") }

// BackupDir holds database copies taken before migrations.
func (l Layout) BackupDir() string { return filepath.Join(l.DataDir, "backups") }

// ThumbsDir holds one small JPEG per file that has a thumbnail, as ab/<id>.jpg.
func (l Layout) ThumbsDir() string { return filepath.Join(l.DataDir, "thumbs") }

// FSInfo describes the drive a folder is on.
type FSInfo struct {
	Type   string
	Free   int64
	Total  int64
	Device uint64
}

// Init creates the folders and the marker. It's meant to be run once, by hand, while the
// drive is mounted.
func Init(l Layout) error {
	for _, dir := range []string{l.StorageDir, l.UploadsDir(), l.TrashDir(), l.DataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	marker := filepath.Join(l.StorageDir, MarkerName)
	if _, err := os.Stat(marker); errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(marker, []byte("This folder is used by Share. Keep this file: the server waits for it before it starts.\n"), 0o644)
	} else {
		return err
	}
}

// WaitForMarker blocks until the storage folder has its marker, logging now and then.
func WaitForMarker(ctx context.Context, storageDir string, logf func(string, ...any)) error {
	marker := filepath.Join(storageDir, MarkerName)
	start := time.Now()
	lastLog := time.Time{}
	for {
		if _, err := os.Stat(marker); err == nil {
			return nil
		}
		if time.Since(lastLog) >= time.Minute {
			logf("storage: waiting for %s (is the drive or volume mounted? run `share init` once)", marker)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for %s after %v: %w", marker, time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}

// Finding is something Check found. Code names it for the website and the app, which
// translate it (contract/storage_warnings.json); Message says it for the console and the log.
type Finding struct {
	Code    string
	Message string
}

func (f Finding) String() string { return f.Message }

// Report is the result of Check.
type Report struct {
	Storage  FSInfo
	Data     FSInfo
	Problems []Finding // the server won't work well until these are fixed
	Warnings []Finding
	// MaxFileSize is the largest file the drive can hold (FAT32: 4 GiB − 1); 0 means no limit.
	MaxFileSize int64
}

// Check inspects the folders and the drives under them. minFree is the space uploads leave
// free on the storage drive.
func Check(l Layout, minFree int64) Report {
	var r Report
	problem := func(code, format string, args ...any) {
		r.Problems = append(r.Problems, Finding{code, fmt.Sprintf(format, args...)})
	}
	warn := func(code, format string, args ...any) {
		r.Warnings = append(r.Warnings, Finding{code, fmt.Sprintf(format, args...)})
	}

	if _, err := os.Stat(filepath.Join(l.StorageDir, MarkerName)); err != nil {
		problem("marker_missing", "%s has no %s marker; mount the drive or volume and run `share init`", l.StorageDir, MarkerName)
		return r
	}
	var err error
	if r.Storage, err = Stat(l.StorageDir); err != nil {
		problem("storage_unreadable", "storage folder: %v", err)
		return r
	}
	for _, dir := range []string{l.UploadsDir(), l.TrashDir()} {
		info, err := Stat(dir)
		switch {
		case err != nil:
			problem("folder_missing", "%s is missing; run `share init`", dir)
		case info.Device != r.Storage.Device:
			problem("other_drive", "%s is on another drive than %s; finishing uploads needs both on one drive", dir, l.StorageDir)
		}
	}
	switch r.Storage.Type {
	case "vfat":
		warn("fat32", "the storage drive is FAT32: files over 4 GiB can't be stored; ext4 is recommended")
		r.MaxFileSize = 4<<30 - 1
	case "exfat", "ntfs", "ntfs3", "fuseblk":
		warn("ignores_case", "the storage drive is %s: it ignores case in names and is slower than ext4", r.Storage.Type)
	case "tmpfs", "squashfs", "overlay", "ubifs", "jffs2":
		problem("not_a_drive", "the storage folder is on %s: memory, flash or a container's own layer, not a drive or a volume", r.Storage.Type)
	}

	if err := os.MkdirAll(l.DataDir, 0o700); err != nil {
		problem("data_unreadable", "data folder: %v", err)
		return r
	}
	if r.Data, err = Stat(l.DataDir); err != nil {
		problem("data_unreadable", "data folder: %v", err)
		return r
	}
	switch r.Data.Type {
	case "fuseblk", "nfs", "cifs", "smb2":
		problem("data_unsafe", "the data folder is on %s; SQLite isn't safe there — set data_dir to a local disk", r.Data.Type)
	case "tmpfs":
		problem("data_in_memory", "the data folder is in memory (tmpfs) and would be lost on restart")
	case "overlay":
		problem("data_in_memory", "the data folder is inside the container (overlay) and would be lost when the container is made anew; put it on a volume")
	}
	// Uploads stop where they would leave less than minFree; say so a while before.
	switch free := r.Storage.Free; {
	case free <= minFree:
		problem("drive_full", "the storage drive is full: %s free, and uploads leave %s (min_free_space_mib)", gib(free), gib(minFree))
	case free < minFree+1<<30:
		warn("low_space", "only %s free on the storage drive; uploads stop at %s (min_free_space_mib)", gib(free), gib(minFree))
	}
	return r
}

func gib(b int64) string { return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30)) }
