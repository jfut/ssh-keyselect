//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/winpath"
)

// guiSameListenEndpoint compares aliases that address the same Windows filesystem path.
func guiSameListenEndpoint(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstPipe, secondPipe := winpath.IsNamedPipe(first), winpath.IsNamedPipe(second)
	if firstPipe || secondPipe {
		return firstPipe && secondPipe && strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
	}
	firstNative, firstErr := winpath.NativeSocketPath(first)
	secondNative, secondErr := winpath.NativeSocketPath(second)
	if firstErr == nil && secondErr == nil {
		return strings.EqualFold(filepath.Clean(firstNative), filepath.Clean(secondNative))
	}
	return guiSamePath(first, second)
}

// guiListenPathExists checks filesystem endpoints and leaves named pipes to the pipe listener.
func guiListenPathExists(endpoint string) (bool, error) {
	if endpoint == "" || winpath.IsNamedPipe(endpoint) {
		return false, nil
	}
	path, err := winpath.NativeSocketPath(endpoint)
	if err != nil {
		return false, nil
	}
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}
