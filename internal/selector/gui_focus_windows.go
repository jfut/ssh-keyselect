//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	pickerUser32              = windows.NewLazySystemDLL("user32.dll")
	pickerAttachThreadInput   = pickerUser32.NewProc("AttachThreadInput")
	pickerBringWindowToTop    = pickerUser32.NewProc("BringWindowToTop")
	pickerSetForegroundWindow = pickerUser32.NewProc("SetForegroundWindow")
	pickerSetFocus            = pickerUser32.NewProc("SetFocus")
)

// acquirePickerNativeFocus bridges Windows foreground restrictions before focusing a MyGo picker window.
func acquirePickerNativeFocus(window *mygo.Window) {
	if window == nil || window.IsDestroyed() {
		return
	}
	hwnd := window.NativeHandle()
	if hwnd == 0 {
		return
	}

	foreground := windows.GetForegroundWindow()
	ourThread := windows.GetCurrentThreadId()
	foregroundThread, _ := windows.GetWindowThreadProcessId(foreground, nil)
	attached := foreground != 0 && foregroundThread != 0 && foregroundThread != ourThread
	if attached {
		result, _, _ := pickerAttachThreadInput.Call(uintptr(ourThread), uintptr(foregroundThread), 1)
		attached = result != 0
	}
	if attached {
		defer pickerAttachThreadInput.Call(uintptr(ourThread), uintptr(foregroundThread), 0)
	}

	// MyGo's Window.Focus calls SetForegroundWindow directly. Temporarily sharing the foreground input queue lets the
	// signature-request dialog take focus when an SSH terminal owns the foreground, as the earlier UI did on Windows.
	pickerBringWindowToTop.Call(hwnd)
	pickerSetForegroundWindow.Call(hwnd)
	pickerSetFocus.Call(hwnd)
}
