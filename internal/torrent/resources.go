package torrent

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ResourceStats struct {
	SampledAt      time.Time `json:"sampled_at"`
	RSSBytes       *uint64   `json:"rss_bytes"`
	HeapAllocBytes uint64    `json:"heap_alloc_bytes"`
	HeapInUseBytes uint64    `json:"heap_in_use_bytes"`
	GoSysBytes     uint64    `json:"go_sys_bytes"`
	Goroutines     int       `json:"goroutines"`
}

func resourceSnapshot() ResourceStats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	stats := ResourceStats{SampledAt: time.Now(), HeapAllocBytes: mem.HeapAlloc,
		HeapInUseBytes: mem.HeapInuse, GoSysBytes: mem.Sys, Goroutines: runtime.NumGoroutine()}
	// Linux exposes current resident pages. Other platforms report null, not a
	// misleading peak-RSS value or zero. Filesystem usage is reported separately.
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/self/statm"); err == nil {
			stats.RSSBytes = parseResidentBytes(string(data), uint64(os.Getpagesize()))
		}
	}
	return stats
}

func parseResidentBytes(data string, pageSize uint64) *uint64 {
	fields := strings.Fields(data)
	if len(fields) < 2 || pageSize == 0 {
		return nil
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || pages > ^uint64(0)/pageSize {
		return nil
	}
	bytes := pages * pageSize
	return &bytes
}
