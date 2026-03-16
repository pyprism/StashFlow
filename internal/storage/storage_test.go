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

func TestStatInvalidPath(t *testing.T) {
	_, err := Stat("/nonexistent/path/that/should/not/exist/xyz", 0.9)
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestCanFitExactBoundary(t *testing.T) {
	stats := &Stats{MaxUsageBytes: 1000, AvailableForNew: 500}

	// Exact fit at the boundary.
	ok, msg := CanFit(stats, 500)
	if !ok || msg != "" {
		t.Errorf("expected exact fit, got fit=%v msg=%q", ok, msg)
	}

	// One byte over available.
	ok, msg = CanFit(stats, 501)
	if ok {
		t.Error("expected one-over to fail")
	}
	if msg != "not enough storage available" {
		t.Errorf("wrong message: %q", msg)
	}

	// Exact at max usage — fits if AvailableForNew allows.
	stats2 := &Stats{MaxUsageBytes: 1000, AvailableForNew: 1000}
	ok, msg = CanFit(stats2, 1000)
	if !ok || msg != "" {
		t.Errorf("expected exact max usage to fit, got fit=%v msg=%q", ok, msg)
	}

	// One byte over max usage.
	ok, msg = CanFit(stats2, 1001)
	if ok {
		t.Error("expected one-over max to fail")
	}
	if msg != "torrent size exceeds the max allowed storage" {
		t.Errorf("wrong message: %q", msg)
	}
}

func TestCanFitZeroSize(t *testing.T) {
	stats := &Stats{MaxUsageBytes: 1000, AvailableForNew: 500}
	ok, msg := CanFit(stats, 0)
	if !ok || msg != "" {
		t.Errorf("zero size should fit, got fit=%v msg=%q", ok, msg)
	}
}

func TestCanFitZeroAvailable(t *testing.T) {
	stats := &Stats{MaxUsageBytes: 1000, AvailableForNew: 0}
	ok, _ := CanFit(stats, 1)
	if ok {
		t.Error("expected no fit when AvailableForNew is 0")
	}
}

func TestStatDifferentPercentages(t *testing.T) {
	dir := t.TempDir()

	stats50, err := Stat(dir, 0.50)
	if err != nil {
		t.Fatalf("Stat(0.50) error: %v", err)
	}
	stats90, err := Stat(dir, 0.90)
	if err != nil {
		t.Fatalf("Stat(0.90) error: %v", err)
	}

	if stats50.MaxUsageBytes >= stats90.MaxUsageBytes {
		t.Error("50% max should be less than 90% max on the same volume")
	}
	if stats50.MaxUsagePct != 0.50 {
		t.Errorf("expected pct=0.50, got %f", stats50.MaxUsagePct)
	}
	if stats90.MaxUsagePct != 0.90 {
		t.Errorf("expected pct=0.90, got %f", stats90.MaxUsagePct)
	}

	// Free bytes should be the same for both.
	if stats50.FreeBytes != stats90.FreeBytes {
		t.Errorf("FreeBytes should match across different pct values")
	}
	// Total should be the same.
	if stats50.TotalBytes != stats90.TotalBytes {
		t.Errorf("TotalBytes should match across different pct values")
	}
}

func TestStatFieldsConsistency(t *testing.T) {
	dir := t.TempDir()
	stats, err := Stat(dir, 0.80)
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	// used + free should approximate total.
	sum := stats.UsedBytes + stats.FreeBytes
	if sum != stats.TotalBytes {
		t.Errorf("used(%d) + free(%d) != total(%d)", stats.UsedBytes, stats.FreeBytes, stats.TotalBytes)
	}

	// MaxUsageBytes should be 80% of total.
	expected := uint64(float64(stats.TotalBytes) * 0.80)
	if stats.MaxUsageBytes != expected {
		t.Errorf("MaxUsageBytes=%d, expected=%d", stats.MaxUsageBytes, expected)
	}
}

