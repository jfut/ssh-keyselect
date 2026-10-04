//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

func guiSameListenEndpoint(first, second string) bool {
	return guiSamePath(first, second)
}
