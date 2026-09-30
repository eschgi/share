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
// instead of creating the folder itself: if the USB drive isn't mounted yet, creating the
// folder would quietly put every upload on the router's flash.
const MarkerName = ".share-storage"

// Layout names the folders.
type Layout struct {
	StorageDir string // the library, one folder per upload day
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
			logf("storage: waiting for %s (is the drive mounted? run `share init` once)", marker)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for %s after %v: %w", marker, time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}

// Report is the result of Check.
type Report struct {
	Storage  FSInfo
	Data     FSInfo
	Problems []string // the server won't work well until these are fixed
	Warnings []string
	// MaxFileSize is the largest file the drive can hold (FAT32: 4 GiB − 1); 0 means no limit.
	MaxFileSize int64
}

// Check inspects the folders and the drives under them.
func Check(l Layout) Report {
	var r Report
	problem := func(format string, args ...any) { r.Problems = append(r.Problems, fmt.Sprintf(format, args...)) }
	warn := func(format string, args ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...)) }

	if _, err := os.Stat(filepath.Join(l.StorageDir, MarkerName)); err != nil {
		problem("%s has no %s marker; mount the drive and run `share init`", l.StorageDir, MarkerName)
		return r
	}
	var err error
	if r.Storage, err = Stat(l.StorageDir); err != nil {
		problem("storage folder: %v", err)
		return r
	}
	for _, dir := range []string{l.UploadsDir(), l.TrashDir()} {
		info, err := Stat(dir)
		switch {
		case err != nil:
			problem("%s is missing; run `share init`", dir)
		case info.Device != r.Storage.Device:
			problem("%s is on another drive than %s; finishing uploads needs both on one drive", dir, l.StorageDir)
		}
	}
	switch r.Storage.Type {
	case "vfat":
		warn("the storage drive is FAT32: files over 4 GiB can't be stored; ext4 is recommended")
		r.MaxFileSize = 4<<30 - 1
	case "exfat", "ntfs", "ntfs3", "fuseblk":
		warn("the storage drive is %s: it ignores case in names and is slower than ext4", r.Storage.Type)
	case "tmpfs", "squashfs", "overlay", "ubifs", "jffs2":
		problem("the storage folder is on %s, which is the router's memory or flash, not a drive", r.Storage.Type)
	}

	if err := os.MkdirAll(l.DataDir, 0o700); err != nil {
		problem("data folder: %v", err)
		return r
	}
	if r.Data, err = Stat(l.DataDir); err != nil {
		problem("data folder: %v", err)
		return r
	}
	switch r.Data.Type {
	case "fuseblk", "nfs", "cifs", "smb2":
		problem("the data folder is on %s; SQLite isn't safe there — set data_dir to a local disk", r.Data.Type)
	case "tmpfs":
		problem("the data folder is in memory (tmpfs) and would be lost on restart")
	}
	if r.Storage.Free < 1<<30 {
		warn("less than 1 GiB free on the storage drive")
	}
	return r
}
