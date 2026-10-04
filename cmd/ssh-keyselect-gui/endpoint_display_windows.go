//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"path/filepath"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

// guiDisplayEndpointPath formats endpoint paths with native Windows separators;
// the original endpoint remains unchanged for connection and copy operations.
func guiDisplayEndpointPath(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	if winpath.IsNamedPipe(endpoint) {
		return guiWindowsPathForDisplay(endpoint)
	}
	lower := strings.ToLower(endpoint)
	if strings.HasPrefix(lower, "/cygdrive/") && len(endpoint) >= 11 &&
		(len(endpoint) == 11 || endpoint[11] == '/') {
		return guiWindowsPathForDisplay(filepath.Clean(strings.ToUpper(endpoint[10:11]) + ":" + filepath.FromSlash(endpoint[11:])))
	}
	if native, err := winpath.NativeSocketPath(endpoint); err == nil {
		return guiWindowsPathForDisplay(native)
	}
	return guiWindowsPathForDisplay(filepath.Clean(filepath.FromSlash(endpoint)))
}

// guiEndpointPathFromDisplay reverses the yen-style separators used in Windows text fields.
func guiEndpointPathFromDisplay(endpoint string) string {
	if winpath.IsNamedPipe(endpoint) {
		return endpoint
	}
	return strings.ReplaceAll(endpoint, "¥", `\`)
}

func guiWindowsPathForDisplay(path string) string {
	if len(path) >= 2 && path[1] == ':' {
		path = strings.ToUpper(path[:1]) + path[1:]
	}
	return strings.ReplaceAll(path, `\`, "¥")
}

// guiListenPathForConfig writes filesystem endpoints in native Windows form while preserving named pipes.
func guiListenPathForConfig(endpoint string) string {
	if endpoint == "" || winpath.IsNamedPipe(endpoint) {
		return endpoint
	}
	native, err := winpath.NativeSocketPath(endpoint)
	if err != nil {
		return endpoint
	}
	if len(native) >= 2 && native[1] == ':' {
		native = strings.ToUpper(native[:1]) + native[1:]
	}
	return native
}

// guiNormalizeListenPathForMode converts a stored native path to the shell form expected by its client transport.
func guiNormalizeListenPathForMode(endpoint string, mode transport.Mode) string {
	if !guiIsNativeWindowsDrivePath(endpoint) {
		return endpoint
	}
	switch mode {
	case transport.Cygwin, transport.WSL1:
		native, err := winpath.NativeSocketPath(endpoint)
		if err != nil {
			return endpoint
		}
		shellPath, err := guiWindowsTransportPath(native, mode)
		if err != nil {
			return endpoint
		}
		return shellPath
	default:
		return endpoint
	}
}

func guiIsNativeWindowsDrivePath(endpoint string) bool {
	return len(endpoint) >= 3 &&
		((endpoint[0] >= 'A' && endpoint[0] <= 'Z') || (endpoint[0] >= 'a' && endpoint[0] <= 'z')) &&
		endpoint[1] == ':' && (endpoint[2] == '\\' || endpoint[2] == '/')
}
