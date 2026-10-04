//go:build gui && darwin

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"os"

	"github.com/ebitengine/purego/objc"
)

// objcForegroundTarget identifies the application that had focus before the picker opened.
type objcForegroundTarget struct {
	processID uint32
}

const objcApplicationActivateIgnoringOtherApps = uintptr(1 << 1)

var (
	objcWorkspaceClass                   = objc.GetClass("NSWorkspace")
	objcRunningApplicationClass          = objc.GetClass("NSRunningApplication")
	objcSharedWorkspaceSelector          = objc.RegisterName("sharedWorkspace")
	objcFrontmostApplicationSelector     = objc.RegisterName("frontmostApplication")
	objcProcessIdentifierSelector        = objc.RegisterName("processIdentifier")
	objcRunningApplicationForPIDSelector = objc.RegisterName("runningApplicationWithProcessIdentifier:")
	objcActivateWithOptionsSelector      = objc.RegisterName("activateWithOptions:")
)

// capturePreviousForegroundWindow saves the frontmost app's process ID before the picker becomes active.
func capturePreviousForegroundWindow() objcForegroundTarget {
	if objcWorkspaceClass == 0 {
		return objcForegroundTarget{}
	}
	workspace := objc.ID(objcWorkspaceClass).Send(objcSharedWorkspaceSelector)
	if workspace == 0 {
		return objcForegroundTarget{}
	}
	application := workspace.Send(objcFrontmostApplicationSelector)
	if application == 0 {
		return objcForegroundTarget{}
	}
	processID := objc.Send[int32](application, objcProcessIdentifierSelector)
	if processID <= 0 || int(processID) == os.Getpid() {
		return objcForegroundTarget{}
	}
	return objcForegroundTarget{processID: uint32(processID)}
}

// restorePreviousForegroundWindow activates the app that was frontmost before the picker opened.
func restorePreviousForegroundWindow(target objcForegroundTarget) {
	if target.processID == 0 || objcRunningApplicationClass == 0 {
		return
	}
	application := objc.ID(objcRunningApplicationClass).Send(objcRunningApplicationForPIDSelector, int32(target.processID))
	if application != 0 {
		objc.Send[bool](application, objcActivateWithOptionsSelector, objcApplicationActivateIgnoringOtherApps)
	}
}
