//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"path/filepath"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

// guiDisplayEndpointPath formats endpoint paths with native Unix separators.
func guiDisplayEndpointPath(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	return filepath.Clean(endpoint)
}

// guiEndpointPathFromDisplay leaves Unix paths unchanged after display formatting.
func guiEndpointPathFromDisplay(endpoint string) string { return endpoint }

func guiDisplayFilePath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func guiFilePathFromDisplay(path string) string { return path }

func guiListenPathForConfig(endpoint string) string { return endpoint }

func guiNormalizeListenPathForMode(endpoint string, _ transport.Mode) string { return endpoint }
