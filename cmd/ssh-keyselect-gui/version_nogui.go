//go:build !gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

// Non-GUI builds use GoReleaser's version linker flag and do not have MyGo metadata.
func resolvePackagedVersion() {}
