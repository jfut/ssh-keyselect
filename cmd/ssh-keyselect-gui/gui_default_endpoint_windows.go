//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

const defaultGUIListenPipe = `\\.\pipe\ssh-keyselect-agent.socket`

// guiDefaultListenEndpoint uses a home-directory socket for filesystem transports and a fixed named pipe for OpenSSH.
func guiDefaultListenEndpoint(upstream string, requested transport.Mode) (string, transport.Mode, error) {
	if requested == transport.NamedPipe {
		return defaultGUIListenPipe, transport.NamedPipe, nil
	}
	mode, err := listener.ResolveMode("", upstream, requested)
	if err != nil {
		return "", "", err
	}
	// An unset path defaults to a filesystem socket until OpenSSH pipe mode is explicitly selected.
	if requested == transport.Auto && mode == transport.NamedPipe {
		mode = transport.Unix
	}
	home := os.Getenv("HOME")
	if mode == transport.WSL1 || filepath.VolumeName(home) == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("locate user home directory: %w", err)
		}
	}
	path := filepath.Join(home, ".ssh", "ssh-keyselect-agent.sock")
	return guiPathForWindowsTransport(path, mode, os.Getenv("HOME"))
}

func guiPathForWindowsTransport(path string, mode transport.Mode, home string) (string, transport.Mode, error) {
	if winpath.IsNamedPipe(path) {
		return path, mode, nil
	}
	if mode == transport.Cygwin && winpath.IsGitBashPath(home) {
		return strings.TrimRight(home, "/") + "/.ssh/ssh-keyselect-agent.sock", mode, nil
	}
	converted, err := guiWindowsTransportPath(path, mode)
	return converted, mode, err
}

func guiEndpointPathForDialog(path string) string {
	if path == "" {
		return ""
	}
	if winpath.IsNamedPipe(path) {
		return ""
	}
	if native, err := winpath.NativeSocketPath(path); err == nil {
		return native
	}
	return path
}

func guiEndpointPathFromDialog(path string, requested transport.Mode, upstream string) (string, error) {
	if path == "" {
		return "", nil
	}
	mode := requested
	if mode == transport.Auto {
		var err error
		mode, err = listener.ResolveMode("", upstream, transport.Auto)
		if err != nil {
			return "", err
		}
		// A filesystem path selected in Auto mode uses the filesystem transport unless it identifies a shell socket.
		if mode == transport.NamedPipe {
			mode = transport.Unix
		}
	}
	return guiWindowsTransportPath(path, mode)
}

// guiWindowsTransportPath converts a native endpoint to the path syntax expected by its shell transport.
func guiWindowsTransportPath(path string, mode transport.Mode) (string, error) {
	switch mode {
	case transport.Cygwin:
		return winpath.ToGitBashPath(path)
	case transport.WSL1:
		converted, err := winpath.ToGitBashPath(path)
		if err != nil {
			return "", err
		}
		return "/mnt" + converted, nil
	default:
		return path, nil
	}
}

func guiEndpointPathIsPipe(path string) bool {
	return winpath.IsNamedPipe(guiEndpointPathFromDisplay(path))
}
