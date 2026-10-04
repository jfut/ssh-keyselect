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
