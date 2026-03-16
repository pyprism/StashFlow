package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.MaxUsagePercent != 0.90 {
		t.Errorf("expected MaxUsagePercent=0.90, got %f", cfg.MaxUsagePercent)
	}
	if cfg.StoragePath != "" {
		t.Errorf("expected empty StoragePath, got %q", cfg.StoragePath)
	}
	if cfg.Port != 0 {
		t.Errorf("expected Port=0, got %d", cfg.Port)
	}
}

func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}

	if !filepath.IsAbs(dir) {
		t.Errorf("ConfigDir() should return absolute path, got %q", dir)
	}
}

func TestPaths(t *testing.T) {
	tests := []struct {
		name string
		fn   func() (string, error)
	}{
		{"ConfigPath", ConfigPath},
		{"StatePath", StatePath},
		{"TorrentDir", TorrentDir},
		{"PidPath", PidPath},
		{"LogPath", LogPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := tt.fn()
			if err != nil {
				t.Fatalf("%s() error: %v", tt.name, err)
			}
			if !filepath.IsAbs(path) {
				t.Errorf("%s() should return absolute path, got %q", tt.name, path)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "stashflow-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testPath := filepath.Join(tmpDir, "config.json")

	// Test config
	cfg := &Config{
		StoragePath:     "/test/storage",
		Port:            8080,
		MaxUsagePercent: 0.85,
	}

	// Test Save
	if err := SaveToPath(cfg, testPath); err != nil {
		t.Fatalf("SaveToPath() error: %v", err)
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	var savedCfg Config
	if err := json.Unmarshal(data, &savedCfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if savedCfg.StoragePath != cfg.StoragePath {
		t.Errorf("expected StoragePath=%q, got %q", cfg.StoragePath, savedCfg.StoragePath)
	}
	if savedCfg.Port != cfg.Port {
		t.Errorf("expected Port=%d, got %d", cfg.Port, savedCfg.Port)
	}
	if savedCfg.MaxUsagePercent != cfg.MaxUsagePercent {
		t.Errorf("expected MaxUsagePercent=%f, got %f", cfg.MaxUsagePercent, savedCfg.MaxUsagePercent)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stashflow-home-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)

	cfg, path, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.MaxUsagePercent != 0.90 {
		t.Errorf("expected MaxUsagePercent=0.90, got %f", cfg.MaxUsagePercent)
	}
	if path == "" {
		t.Fatal("expected config path, got empty string")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stashflow-home-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)

	cfg := &Config{
		StoragePath:     "/tmp/stashflow",
		Port:            9090,
		MaxUsagePercent: 0.75,
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, _, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Errorf("loaded config differs from saved config.\nwant: %+v\ngot:  %+v", cfg, loaded)
	}
}
