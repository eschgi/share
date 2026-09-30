//go:build linux

package storage

import (
	"os"
	"syscall"
)

// fsTypes names the file systems that matter here, by statfs magic number.
var fsTypes = map[int64]string{
	0xEF53:     "ext4",
	0x4d44:     "vfat",
	0x2011BAB0: "exfat",
	0x7366746e: "ntfs3",
	0x5346544e: "ntfs",
	0x65735546: "fuseblk", // ntfs-3g and other FUSE drives
	0x9123683E: "btrfs",
	0x58465342: "xfs",
	0xF2F52010: "f2fs",
	0x2FC12FC1: "zfs",
	0x6969:     "nfs",
	0xFF534D42: "cifs",
	0xFE534D42: "smb2",
	0x01021994: "tmpfs",
	0x73717368: "squashfs",
	0x794c7630: "overlay",
	0x24051905: "ubifs",
	0x72b6:     "jffs2",
}

// Stat returns the file system facts about the drive holding path.
func Stat(path string) (FSInfo, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return FSInfo{}, err
	}
	info := FSInfo{
		Type:  fsTypes[int64(s.Type)],
		Free:  int64(s.Bavail) * int64(s.Bsize),
		Total: int64(s.Blocks) * int64(s.Bsize),
	}
	if info.Type == "" {
		info.Type = "unknown"
	}
	fi, err := os.Stat(path)
	if err != nil {
		return FSInfo{}, err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		info.Device = uint64(st.Dev)
	}
	return info, nil
}
