//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package guiwindow contains shared window-placement behavior for GUI surfaces.
package guiwindow

import (
	"runtime"

	"github.com/richardwilkes/unison"
)

// CenterOnPrimaryDisplay centers a window in the primary display's usable area.
func CenterOnPrimaryDisplay(window *unison.Window) {
	display := unison.PrimaryDisplay()
	if display == nil {
		return
	}
	usable := display.Usable
	// Windows reports display bounds in physical pixels while window sizes use logical units.
	if runtime.GOOS == "windows" && display.Scale.X > 0 && display.Scale.Y > 0 {
		usable.Width /= display.Scale.X
		usable.Height /= display.Scale.Y
	}
	frame := window.FrameRect()
	frame.X = usable.X + (usable.Width-frame.Width)/2
	frame.Y = usable.Y + (usable.Height-frame.Height)/2
	window.SetFrameRect(frame)
	window.EnsureOnDisplay()
}
