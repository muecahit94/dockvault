//go:build unix

package handlers

import "syscall"

type diskUsageInfo struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

func readDiskUsage(dir string) *diskUsageInfo {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return nil
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	free := st.Bavail * bsize
	var used uint64
	if total >= free {
		used = total - free
	}
	return &diskUsageInfo{
		Path:       dir,
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
	}
}
