package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"stashflow/internal/config"
)

func TestStorageSnapshot(t *testing.T) {
	cfg := &config.Config{
		StoragePath:     t.TempDir(),
		MaxUsagePercent: 0.90,
	}

	result, err := storageSnapshot(cfg)
	if err != nil {
		t.Fatalf("storageSnapshot() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestStorageSnapshotInvalidPath(t *testing.T) {
	cfg := &config.Config{
		StoragePath:     "/nonexistent/path/that/does/not/exist",
		MaxUsagePercent: 0.90,
	}

	_, err := storageSnapshot(cfg)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNewAndShutdown(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stashflow-app-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	statePath := filepath.Join(tmpDir, "state.json")
	cfgPath := filepath.Join(tmpDir, "config.json")
	os.MkdirAll(storagePath, 0o755)
	os.MkdirAll(torrentDir, 0o755)

	cfg := &config.Config{
		StoragePath:     storagePath,
		Port:            0,
		MaxUsagePercent: 0.90,
	}

	a, err := New(cfg, cfgPath, statePath, torrentDir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := ShutdownWithTimeout(a, 5*time.Second); err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
}

func TestNewInvalidStoragePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stashflow-app-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	os.MkdirAll(torrentDir, 0o755)
	statePath := filepath.Join(tmpDir, "state.json")
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		StoragePath:     storagePath,
		Port:            0,
		MaxUsagePercent: 0.90,
	}

	a, err := New(cfg, cfgPath, statePath, torrentDir)
	if err != nil {
		return
	}
	_ = ShutdownWithTimeout(a, 2*time.Second)
}

func TestShutdownWithTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stashflow-app-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	statePath := filepath.Join(tmpDir, "state.json")
	cfgPath := filepath.Join(tmpDir, "config.json")
	os.MkdirAll(storagePath, 0o755)
	os.MkdirAll(torrentDir, 0o755)

	cfg := &config.Config{
		StoragePath:     storagePath,
		Port:            0,
		MaxUsagePercent: 0.90,
	}

	a, err := New(cfg, cfgPath, statePath, torrentDir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Shutdown with a very short timeout — should still succeed
	// because the server was never started (Run not called).
	err = ShutdownWithTimeout(a, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("ShutdownWithTimeout error: %v", err)
	}
}

