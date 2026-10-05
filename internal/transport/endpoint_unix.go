//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package transport

import "path/filepath"

// SameEndpoint compares socket paths before binding, even when they do not exist yet.
func SameEndpoint(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	return firstErr == nil && secondErr == nil && firstAbs == secondAbs
}
