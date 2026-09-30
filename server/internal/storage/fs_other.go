//go:build !linux

package storage

import "os"

// Stat is a stand-in on systems other than Linux (the Windows build is for development):
// it knows nothing about the drive, so space checks never block an upload.
func Stat(path string) (FSInfo, error) {
	if _, err := os.Stat(path); err != nil {
		return FSInfo{}, err
	}
	return FSInfo{Type: "unknown", Free: 1 << 50, Total: 1 << 50}, nil
}
