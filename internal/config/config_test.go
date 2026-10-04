// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadAppliesEnvironmentAndDefaults(t *testing.T) {
	t.Setenv("SSH_KEYSELECT_LISTEN", "")
	t.Setenv("UPSTREAM_SSH_AUTH_SOCK", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("KEYSELECT_TEST_PROXY", "/tmp/proxy.sock")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	data := []byte("[agent]\nlisten = \"$KEYSELECT_TEST_PROXY\"\n")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Listen != "/tmp/proxy.sock" || cfg.Agent.Upstream != "" || cfg.Log.Level != "off" {
		t.Fatalf("loaded config = %+v", cfg)
	}
}

func TestEnvironmentOverridesListenAndConfigUpstreamOverridesSocketEnvironment(t *testing.T) {
	t.Setenv("SSH_KEYSELECT_LISTEN", "/tmp/env-proxy.sock")
	t.Setenv("UPSTREAM_SSH_AUTH_SOCK", "/tmp/preferred-agent.sock")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[agent]\nlisten = \"/tmp/config-proxy.sock\"\nupstream = \"/tmp/config-agent.sock\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Listen != "/tmp/env-proxy.sock" || cfg.Agent.Upstream != "/tmp/config-agent.sock" {
		t.Fatalf("unexpected environment/config resolution: %+v", cfg.Agent)
	}
}

func TestSocketEnvironmentProvidesDefaultUpstream(t *testing.T) {
	t.Setenv("SSH_KEYSELECT_LISTEN", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tt := range []struct {
		name      string
		preferred string
		fallback  string
		want      string
	}{
		{
			name:      "preferred variable wins",
			preferred: "/tmp/preferred-agent.sock",
			fallback:  "/tmp/agent.sock",
			want:      "/tmp/preferred-agent.sock",
		},
		{
			name:     "fallback variable is used when preferred variable is empty",
			fallback: "/tmp/agent.sock",
			want:     "/tmp/agent.sock",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UPSTREAM_SSH_AUTH_SOCK", tt.preferred)
			t.Setenv("SSH_AUTH_SOCK", tt.fallback)
			cfg, err := Load("")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Agent.Upstream != tt.want {
				t.Fatalf("upstream = %q, want %q", cfg.Agent.Upstream, tt.want)
			}
			if cfg.Agent.Listen != "" {
				t.Fatalf("listen = %q, want empty for run-time random endpoint", cfg.Agent.Listen)
			}
		})
	}
}

func TestLoadRejectsUnknownConfigurationKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[agent]\nunknown_option = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown configuration key")
	}
}

func TestExpandPathExpandsLeadingHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	if got, want := ExpandPath("~/agent.sock"), filepath.Join(home, "agent.sock"); got != want {
		t.Fatalf("expanded path = %q, want %q", got, want)
	}
}

func TestDefaultPathUsesKeyselectDirectory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "ssh-keyselect", "config.toml"); path != want {
		t.Fatalf("default config path = %q, want %q", path, want)
	}
}

func TestSaveWritesConfigurationThatCanBeLoaded(t *testing.T) {
	t.Setenv("SSH_KEYSELECT_LISTEN", "")
	cfg := Default()
	cfg.Agent.Upstream = `C:\Users\alice\.ssh\agent.sock`
	cfg.Agent.Listen = `C:\Users\alice\.ssh\proxy.sock`
	cfg.Log.Level = "debug"
	path := filepath.Join(t.TempDir(), "settings", "config.toml")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agent.Upstream != cfg.Agent.Upstream || loaded.Agent.Listen != cfg.Agent.Listen || loaded.Log.Level != cfg.Log.Level {
		t.Fatalf("loaded config = %+v, want agent %+v and log %+v", loaded, cfg.Agent, cfg.Log)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("config permissions = %04o, want 0600", info.Mode().Perm())
		}
	}
}
