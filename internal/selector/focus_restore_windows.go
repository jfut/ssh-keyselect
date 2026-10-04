//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// foregroundTarget identifies the window that had focus before the picker opened.
type foregroundTarget struct {
	handle    uintptr
	processID uint32
}

const foregroundRestoreWindow = 9
const foregroundRootAncestor = 2

var (
	foregroundUser32           = windows.NewLazySystemDLL("user32.dll")
	foregroundShowWindowAsync  = foregroundUser32.NewProc("ShowWindowAsync")
	setForegroundWindow        = foregroundUser32.NewProc("SetForegroundWindow")
	foregroundBringWindowToTop = foregroundUser32.NewProc("BringWindowToTop")
	foregroundIsIconicWindow   = foregroundUser32.NewProc("IsIconic")
	foregroundGetAncestor      = foregroundUser32.NewProc("GetAncestor")
)

// capturePreviousForegroundWindow saves the foreground window outside this process before the picker takes focus.
func capturePreviousForegroundWindow() foregroundTarget {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return foregroundTarget{}
	}
	if root, _, _ := foregroundGetAncestor.Call(uintptr(hwnd), foregroundRootAncestor); root != 0 {
		hwnd = windows.HWND(root)
	}
	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &processID); err != nil || processID == uint32(os.Getpid()) {
		return foregroundTarget{}
	}
	return foregroundTarget{handle: uintptr(hwnd), processID: processID}
}

// restorePreviousForegroundWindow returns input focus to the application that was active before key selection.
func restorePreviousForegroundWindow(target foregroundTarget) {
	if target.handle == 0 || target.processID == 0 {
		return
	}
	hwnd := windows.HWND(target.handle)
	if !validForegroundTarget(hwnd, target.processID) {
		return
	}
	if activateForegroundTarget(hwnd, target.processID) {
		return
	}
	// Retry after the window manager processes any pending activation from the modal window.
	time.AfterFunc(75*time.Millisecond, func() {
		current := windows.GetForegroundWindow()
		if current == 0 {
			return
		}
		var currentProcessID uint32
		if _, err := windows.GetWindowThreadProcessId(current, &currentProcessID); err == nil && currentProcessID == uint32(os.Getpid()) {
			activateForegroundTarget(hwnd, target.processID)
		}
	})
}

func validForegroundTarget(hwnd windows.HWND, expectedProcessID uint32) bool {
	if !windows.IsWindow(hwnd) {
		return false
	}
	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &processID); err != nil || processID != expectedProcessID {
		return false
	}
	return true
}

func activateForegroundTarget(hwnd windows.HWND, expectedProcessID uint32) bool {
	if !validForegroundTarget(hwnd, expectedProcessID) {
		return false
	}
	if result, _, _ := foregroundIsIconicWindow.Call(uintptr(hwnd)); result != 0 {
		foregroundShowWindowAsync.Call(uintptr(hwnd), foregroundRestoreWindow)
	}
	foregroundBringWindowToTop.Call(uintptr(hwnd))
	setForegroundWindow.Call(uintptr(hwnd))
	current := windows.GetForegroundWindow()
	if current == 0 {
		return false
	}
	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(current, &processID); err != nil {
		return false
	}
	return processID == expectedProcessID
}
