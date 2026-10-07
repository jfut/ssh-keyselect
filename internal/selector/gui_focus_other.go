//go:build gui && !windows && !linux

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import "github.com/egoist/mygo"

func capturePickerReturnWindow() uintptr { return 0 }

func pickerWindowOwnership(parent *mygo.Window) (*mygo.Window, bool) { return parent, parent != nil }

func acquirePickerNativeFocus(*mygo.Window) {}

func capturePickerRestoreTimestamp() uint32 { return 0 }

func restorePickerReturnWindow(uintptr, uint32) {}

func releasePickerReturnWindow(uintptr) {}
