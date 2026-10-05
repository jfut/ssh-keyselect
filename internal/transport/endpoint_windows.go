//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package transport

import (
	"path/filepath"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/winpath"
)

// SameEndpoint compares pipe names and native paths, including shell path aliases.
func SameEndpoint(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstPipe, secondPipe := winpath.IsNamedPipe(first), winpath.IsNamedPipe(second)
	if firstPipe || secondPipe {
		return firstPipe && secondPipe && strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
	}
	firstNative, firstErr := winpath.NativeSocketPath(first)
	secondNative, secondErr := winpath.NativeSocketPath(second)
	return firstErr == nil && secondErr == nil && strings.EqualFold(firstNative, secondNative)
}
