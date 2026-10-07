//go:build gui && darwin

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// capturePickerReturnWindow remembers the frontmost app before the picker activates KeySelect.
func capturePickerReturnWindow() uintptr {
	var processID uintptr
	mygo.RunOnMain(func() {
		workspace := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace"))
		application := workspace.Send(objc.RegisterName("frontmostApplication"))
		if application == 0 {
			return
		}
		pid := objc.Send[int32](application, objc.RegisterName("processIdentifier"))
		if pid > 0 {
			processID = uintptr(pid)
		}
	})
	return processID
}

func pickerWindowOwnership(parent *mygo.Window) (*mygo.Window, bool) {
	if parent == nil {
		return nil, false
	}
	// A modal sheet makes AppKit restore a minimized parent when it is shown.
	// Keep the picker independent in that state so only the picker appears.
	if parent.IsMinimized() {
		return nil, false
	}
	return parent, true
}

func acquirePickerNativeFocus(*mygo.Window) {}

func capturePickerRestoreTimestamp() uint32 { return 0 }

// restorePickerReturnWindow reactivates the requesting app after AppKit finishes closing the sheet.
func restorePickerReturnWindow(processID uintptr, _ uint32) {
	if processID == 0 {
		return
	}
	time.AfterFunc(150*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			application := objc.ID(objc.GetClass("NSRunningApplication")).Send(
				objc.RegisterName("runningApplicationWithProcessIdentifier:"), int32(processID),
			)
			if application != 0 {
				application.Send(objc.RegisterName("activateWithOptions:"), uintptr(0))
			}
		})
	})
}

func releasePickerReturnWindow(uintptr) {}
