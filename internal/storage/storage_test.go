package storage

import "testing"

func TestCanFit(t *testing.T) {
	stats := &Stats{MaxUsageBytes: 900, AvailableForNew: 600}
	canFit, msg := CanFit(stats, 500)
	if !canFit || msg != "" {
		t.Errorf("expected fit=true, got fit=%v, msg=%q", canFit, msg)
	}
	canFit, msg = CanFit(stats, 700)
	if canFit || msg != "not enough storage available" {
		t.Errorf("expected fit=false, got fit=%v, msg=%q", canFit, msg)
	}
	canFit, msg = CanFit(stats, 1000)
	if canFit || msg != "torrent size exceeds the max allowed storage" {
		t.Errorf("expected fit=false for oversize, got fit=%v, msg=%q", canFit, msg)
	}
}

func TestStatOnTempDir(t *testing.T) {
	dir := t.TempDir()
	stats, err := Stat(dir, 0.9)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if stats.TotalBytes == 0 {
		t.Errorf("expected total bytes > 0")
	}
	if stats.UsedBytes > stats.TotalBytes {
		t.Errorf("used bytes should not exceed total")
	}
	if stats.MaxUsageBytes == 0 {
		t.Errorf("expected max usage bytes > 0")
	}
}
