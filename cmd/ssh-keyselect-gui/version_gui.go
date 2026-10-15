//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import "github.com/egoist/mygo"

// resolvePackagedVersion uses MyGo's app metadata when its build did not set the GoReleaser version variable.
func resolvePackagedVersion() {
	if version != "dev" {
		return
	}
	if packagedVersion := mygo.App.Version(); packagedVersion != "" {
		version = packagedVersion
	}
}
