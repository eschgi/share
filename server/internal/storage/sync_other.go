//go:build !windows

package storage

import (
	"errors"
	"os"
)

// syncFile flushes a file or a folder to the drive.
func syncFile(root *os.Root, name string) error {
	fh, err := root.Open(name)
	if err != nil {
		return err
	}
	defer fh.Close()
	if err := fh.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
