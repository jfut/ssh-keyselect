//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import "path/filepath"

// sameEndpoint detects equivalent paths even when the socket does not exist yet.
func sameEndpoint(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	return firstErr == nil && secondErr == nil && firstAbs == secondAbs
}
