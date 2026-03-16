package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	StoragePath     string  `json:"storage_path"`
	Port            int     `json:"port"`
	MaxUsagePercent float64 `json:"max_usage_percent"`
}

func Default() *Config {
	return &Config{
		MaxUsagePercent: 0.90,
	}
}

func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "stashflow"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func StatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func TorrentDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "torrents"), nil
}

func PidPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "stashflow.pid"), nil
}

func LogPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "stashflow.log"), nil
}

func EnsureDirs() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tdir, err := TorrentDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return err
	}
	return nil
}

func Load() (*Config, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, "", err
	}
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, path, nil
		}
		return nil, "", err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, "", fmt.Errorf("parse config: %w", err)
	}
	if cfg.MaxUsagePercent == 0 {
		cfg.MaxUsagePercent = 0.90
	}
	return cfg, path, nil
}

func Save(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return SaveToPath(cfg, path)
}

func SaveToPath(cfg *Config, path string) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
