//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
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
