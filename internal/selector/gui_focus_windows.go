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
	pickerIsWindow            = pickerUser32.NewProc("IsWindow")
	pickerSetForegroundWindow = pickerUser32.NewProc("SetForegroundWindow")
	pickerSetFocus            = pickerUser32.NewProc("SetFocus")
)

// capturePickerReturnWindow remembers the foreground app that opened the picker.
func capturePickerReturnWindow() uintptr {
	return uintptr(windows.GetForegroundWindow())
}

func pickerWindowOwnership(parent *mygo.Window) (*mygo.Window, bool) { return parent, parent != nil }

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
		defer func() {
			_, _, _ = pickerAttachThreadInput.Call(uintptr(ourThread), uintptr(foregroundThread), 0)
		}()
	}

	// MyGo's Window.Focus calls SetForegroundWindow directly. Temporarily sharing the foreground input queue lets the
	// signature-request dialog take focus when an SSH terminal owns the foreground, as the earlier UI did on Windows.
	// The desktop may still deny activation, so these native focus calls are best-effort.
	_, _, _ = pickerBringWindowToTop.Call(hwnd)
	_, _, _ = pickerSetForegroundWindow.Call(hwnd)
	_, _, _ = pickerSetFocus.Call(hwnd)
}

// restorePickerReturnWindow returns keyboard focus to the app that requested a key selection.
func capturePickerRestoreTimestamp() uint32 { return 0 }

func restorePickerReturnWindow(hwnd uintptr, _ uint32) {
	if hwnd == 0 {
		return
	}
	if result, _, _ := pickerIsWindow.Call(hwnd); result == 0 {
		return
	}

	ourThread := windows.GetCurrentThreadId()
	returnThread, _ := windows.GetWindowThreadProcessId(windows.HWND(hwnd), nil)
	attached := returnThread != 0 && returnThread != ourThread
	if attached {
		result, _, _ := pickerAttachThreadInput.Call(uintptr(ourThread), uintptr(returnThread), 1)
		attached = result != 0
	}
	if attached {
		defer func() {
			_, _, _ = pickerAttachThreadInput.Call(uintptr(ourThread), uintptr(returnThread), 0)
		}()
	}

	// Returning focus is also best-effort because foreground changes are controlled by Windows.
	_, _, _ = pickerBringWindowToTop.Call(hwnd)
	_, _, _ = pickerSetForegroundWindow.Call(hwnd)
	_, _, _ = pickerSetFocus.Call(hwnd)
}

func releasePickerReturnWindow(uintptr) {}
