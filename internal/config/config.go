package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Client struct {
	DeviceID                string `json:"device_id"`
	ServerURL               string `json:"server_url"`
	CodexDir                string `json:"codex_dir"`
	IncludeArchivedSessions bool   `json:"include_archived_sessions"`
	DeviceKeyID             string `json:"device_key_id"`
	DevicePrivateKey        string `json:"device_private_key"`
	ServerKeyID             string `json:"server_key_id"`
	ServerPublicKey         string `json:"server_public_key"`
}

type Device struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Enabled   bool   `json:"enabled"`
}

type Server struct {
	Listen            string            `json:"listen"`
	Database          string            `json:"database"`
	ServerPrivateKeys map[string]string `json:"server_private_keys"`
	Devices           map[string]Device `json:"devices"`
}

func LoadClient(path string) (Client, error) {
	var cfg Client
	if err := load(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.DeviceID == "" || cfg.ServerURL == "" || cfg.CodexDir == "" || cfg.DeviceKeyID == "" || cfg.DevicePrivateKey == "" || cfg.ServerKeyID == "" || cfg.ServerPublicKey == "" {
		return cfg, errors.New("client config requires device_id, server_url, codex_dir, device_key_id, device_private_key, server_key_id, and server_public_key")
	}
	cfg.ServerURL = strings.TrimRight(cfg.ServerURL, "/")
	cfg.CodexDir = expandHome(cfg.CodexDir)
	cfg.DevicePrivateKey = resolveRelative(path, cfg.DevicePrivateKey)
	cfg.ServerPublicKey = resolveRelative(path, cfg.ServerPublicKey)
	return cfg, nil
}

func LoadServer(path string) (Server, error) {
	var cfg Server
	if err := load(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if cfg.Database == "" || len(cfg.ServerPrivateKeys) == 0 || len(cfg.Devices) == 0 {
		return cfg, errors.New("server config requires database, server_private_keys, and devices")
	}
	cfg.Database = resolveRelative(path, cfg.Database)
	for id, keyPath := range cfg.ServerPrivateKeys {
		if id == "" || keyPath == "" {
			return cfg, errors.New("server private key IDs and paths must not be empty")
		}
		cfg.ServerPrivateKeys[id] = resolveRelative(path, keyPath)
	}
	for id, device := range cfg.Devices {
		if id == "" || device.KeyID == "" || device.PublicKey == "" {
			return cfg, fmt.Errorf("device %q requires key_id and public_key", id)
		}
		device.PublicKey = resolveRelative(path, device.PublicKey)
		cfg.Devices[id] = device
	}
	return cfg, nil
}

func load(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func resolveRelative(configPath, value string) string {
	value = expandHome(value)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(filepath.Dir(configPath), value)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
