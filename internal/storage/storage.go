package storage

import (
	"fmt"
	"syscall"
)

type Stats struct {
	TotalBytes      uint64  `json:"total_bytes"`
	FreeBytes       uint64  `json:"free_bytes"`
	UsedBytes       uint64  `json:"used_bytes"`
	MaxUsagePct     float64 `json:"max_usage_pct"`
	MaxUsageBytes   uint64  `json:"max_usage_bytes"`
	AvailableForNew uint64  `json:"available_for_new"`
}

func Stat(path string, maxUsagePct float64) (*Stats, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return nil, fmt.Errorf("statfs: %w", err)
	}
	total := fs.Blocks * uint64(fs.Bsize)
	free := fs.Bavail * uint64(fs.Bsize)
	used := total - free
	maxUsage := uint64(float64(total) * maxUsagePct)
	var available uint64
	if maxUsage > used {
		available = maxUsage - used
	}
	return &Stats{
		TotalBytes:      total,
		FreeBytes:       free,
		UsedBytes:       used,
		MaxUsagePct:     maxUsagePct,
		MaxUsageBytes:   maxUsage,
		AvailableForNew: available,
	}, nil
}

func CanFit(stats *Stats, size uint64) (bool, string) {
	if size > stats.MaxUsageBytes {
		return false, "torrent size exceeds the max allowed storage"
	}
	if size > stats.AvailableForNew {
		return false, "not enough storage available"
	}
	return true, ""
}
