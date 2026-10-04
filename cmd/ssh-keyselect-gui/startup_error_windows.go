//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"syscall"
	"unsafe"

	"github.com/jfut/ssh-keyselect/internal/branding"
)

// showStartupError displays startup failures when the GUI binary has no console.
func showStartupError(message string) {
	text, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	title, err := syscall.UTF16PtrFromString(branding.Name)
	if err != nil {
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
