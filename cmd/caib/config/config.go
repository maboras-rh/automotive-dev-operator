// Package config provides local CLI configuration (e.g. default server URL) for caib.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDir  = ".caib"
	configFile = "cli.json"
)

// CLIConfig holds saved CLI settings.
type CLIConfig struct {
	ServerURL string `json:"server_url"`
}

// DefaultServer returns the effective default server URL: CAIB_SERVER env, then saved config.
func DefaultServer() string {
	if s := strings.TrimSpace(os.Getenv("CAIB_SERVER")); s != "" {
		return s
	}
	cfg, err := Read()
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.ServerURL)
}

// Read reads the CLI config from the user's home directory.
func Read() (*CLIConfig, error) {
	dir, err := configDirPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg CLIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveServerURL writes the given server URL to the local config file.
func SaveServerURL(serverURL string) error {
	dir, err := configDirPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cfg := &CLIConfig{ServerURL: strings.TrimSpace(serverURL)}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, configFile), data, 0600)
}

func configDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir), nil
}
