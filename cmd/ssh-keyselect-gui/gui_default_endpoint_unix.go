//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"os"
	"path/filepath"

	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

// guiFallbackUpstreamEndpoint uses a per-user socket when no agent is configured.
func guiFallbackUpstreamEndpoint(mode transport.Mode) (string, error) {
	return guiDefaultUpstreamEndpoint(mode)
}

// guiDefaultUpstreamEndpoint chooses an endpoint path for the selected transport mode.
func guiDefaultUpstreamEndpoint(mode transport.Mode) (string, error) {
	if mode == transport.NamedPipe {
		return `\\.\pipe\openssh-ssh-agent`, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "ssh-agent.sock"), nil
}

// guiDefaultListenEndpoint uses a predictable per-user socket when no endpoint is configured.
func guiDefaultListenEndpoint(upstream string, requested transport.Mode) (string, transport.Mode, error) {
	mode, err := listener.ResolveMode("", upstream, requested)
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".ssh", "ssh-keyselect-agent.sock"), mode, nil
}

func guiEndpointPathForDialog(path string) string { return path }

func guiEndpointPathFromDialog(path string, _ transport.Mode, _ string) (string, error) {
	if path == "" {
		return "", nil
	}
	return filepath.Clean(path), nil
}

func guiEndpointPathIsPipe(string) bool { return false }
