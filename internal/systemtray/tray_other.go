// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

//go:build !windows

// Package systemtray reports whether a platform has a system tray implementation.
package systemtray

// Supported reports whether this build can create an operating-system tray icon.
func Supported() bool { return false }

// Callbacks connects tray menu choices to the GUI actions.
type Callbacks struct {
	Show    func()
	Quit    func()
	Refresh func()
	IconPNG []byte
}

// Start is a no-op on platforms without an operating-system tray implementation.
func Start(Callbacks) (func() error, string, error) { return func() error { return nil }, "", nil }
