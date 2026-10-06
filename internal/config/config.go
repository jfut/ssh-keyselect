// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package config loads and validates the user's TOML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

type Config struct {
	Agent AgentConfig `toml:"agent"`
	Log   LogConfig   `toml:"log"`
}

type AgentConfig struct {
	Listen       string         `toml:"listen"`
	Upstream     string         `toml:"upstream"`
	UpstreamMode transport.Mode `toml:"upstream_mode"`
	ListenMode   transport.Mode `toml:"listen_mode"`
}

type LogConfig struct {
	File  string `toml:"file"`
	Level string `toml:"level"`
}

type saveConfig struct {
	Agent saveAgentConfig `toml:"agent"`
	Log   LogConfig       `toml:"log"`
}

type saveAgentConfig struct {
	Listen       tomlPath       `toml:"listen"`
	Upstream     tomlPath       `toml:"upstream"`
	UpstreamMode transport.Mode `toml:"upstream_mode"`
	ListenMode   transport.Mode `toml:"listen_mode"`
}

type tomlPath string

// MarshalTOML writes Windows paths as literal strings so their separators stay readable.
func (path tomlPath) MarshalTOML() ([]byte, error) {
	value := string(path)
	if strings.Contains(value, `\`) && !strings.ContainsRune(value, '\'') && isTOMLLiteralString(value) {
		return []byte("'" + value + "'"), nil
	}
	encoded, err := toml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded, []byte("\n")), nil
}

func isTOMLLiteralString(value string) bool {
	for _, char := range value {
		if char < 0x20 && char != '\t' || char >= 0x7f && char <= 0x9f {
			return false
		}
	}
	return true
}

func configForSave(cfg Config) saveConfig {
	return saveConfig{
		Agent: saveAgentConfig{
			Listen:       tomlPath(cfg.Agent.Listen),
			Upstream:     tomlPath(cfg.Agent.Upstream),
			UpstreamMode: cfg.Agent.UpstreamMode,
			ListenMode:   cfg.Agent.ListenMode,
		},
		Log: cfg.Log,
	}
}

// Default returns conservative defaults for GUI and terminal commands.
func Default() Config {
	return Config{
		Agent: AgentConfig{UpstreamMode: transport.Auto, ListenMode: transport.Auto},
		Log:   LogConfig{Level: "off"},
	}
}

// DefaultPath returns the platform's standard per-user TOML configuration path.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locate user config directory: %w", err)
		}
	}
	return filepath.Join(base, "ssh-keyselect", "config.toml"), nil
}

// Load reads an optional TOML file, then resolves the documented environment defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
		if _, err := os.Stat(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("inspect config file: %w", err)
			}
			path = ""
		}
	}
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config file: %w", err)
		}
		decoder := toml.NewDecoder(file)
		metadata, err := decoder.Decode(&cfg)
		if err != nil {
			_ = file.Close()
			return Config{}, fmt.Errorf("parse config file: %w", err)
		}
		if unknown := metadata.Undecoded(); len(unknown) > 0 {
			_ = file.Close()
			return Config{}, fmt.Errorf("config contains unknown keys: %v", unknown)
		}
		if err := file.Close(); err != nil {
			return Config{}, fmt.Errorf("close config file: %w", err)
		}
	}

	if value := os.Getenv("SSH_KEYSELECT_LISTEN"); value != "" {
		cfg.Agent.Listen = value
	}
	if cfg.Agent.Upstream == "" {
		cfg.Agent.Upstream = UpstreamFromEnvironment()
	}
	cfg.Agent.Listen = ExpandPath(cfg.Agent.Listen)
	cfg.Agent.Upstream = ExpandPath(cfg.Agent.Upstream)
	cfg.Log.File = ExpandPath(cfg.Log.File)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// UpstreamFromEnvironment resolves the upstream socket using the documented preference order.
func UpstreamFromEnvironment() string {
	if upstream := os.Getenv("UPSTREAM_SSH_AUTH_SOCK"); upstream != "" {
		return upstream
	}
	return os.Getenv("SSH_AUTH_SOCK")
}

// Save validates and writes the supplied configuration with private file permissions.
func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config file path is empty")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	var encoded bytes.Buffer
	encoder := toml.NewEncoder(&encoded)
	encoder.Indent = ""
	if err := encoder.Encode(configForSave(cfg)); err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("set config file permissions: %w", err)
	}
	return nil
}

// Validate rejects configuration that this release cannot safely honor.
func (c Config) Validate() error {
	if _, err := transport.ParseMode(string(c.Agent.UpstreamMode)); err != nil {
		return fmt.Errorf("agent.upstream_mode: %w", err)
	}
	if _, err := transport.ParseMode(string(c.Agent.ListenMode)); err != nil {
		return fmt.Errorf("agent.listen_mode: %w", err)
	}
	switch strings.ToLower(c.Log.Level) {
	case "off", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be off, debug, info, warn, or error")
	}
	return nil
}

// ExpandPath expands environment variables and a leading home-directory marker.
func ExpandPath(path string) string {
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
			}
		}
	}
	return path
}
