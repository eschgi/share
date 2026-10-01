//go:build windows

package storage

import "os"

// syncFile flushes a file to the drive. Windows flushes only through a handle that may write
// ("Access is denied" otherwise), and has no flush for folders, so those are skipped: NTFS
// keeps the rename in its journal.
func syncFile(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	fh, err := root.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer fh.Close()
	return fh.Sync()
}
