//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import "github.com/egoist/mygo"

func capturePickerReturnWindow() uintptr { return 0 }

func acquirePickerNativeFocus(*mygo.Window) {}

func restorePickerReturnWindow(uintptr) {}
