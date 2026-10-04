//go:build gui && linux && (amd64 || arm64)

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"os"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// foregroundTarget identifies the X11 window that had focus before the picker opened.
type foregroundTarget struct {
	handle uintptr
}

const (
	x11ClientMessage        = 33
	x11SubstructureNotify   = 1 << 19
	x11SubstructureRedirect = 1 << 20
)

type x11ClientMessageEvent struct {
	eventType   int32
	serial      uintptr
	sendEvent   int32
	_           uint32
	display     unsafe.Pointer
	window      uintptr
	messageType uintptr
	format      int32
	_           uint32
	data        [5]uintptr
}

var (
	x11LoadOnce          sync.Once
	x11Library           uintptr
	x11OpenDisplay       func(*byte) unsafe.Pointer
	x11CloseDisplay      func(unsafe.Pointer)
	x11DefaultRootWindow func(unsafe.Pointer) uintptr
	x11InternAtom        func(unsafe.Pointer, *byte, int32) uintptr
	x11GetWindowProperty func(unsafe.Pointer, uintptr, uintptr, int64, int64, int32, uintptr, *uintptr, *int32, *uintptr, *uintptr, *unsafe.Pointer) int32
	x11GetInputFocus     func(unsafe.Pointer, *uintptr, *int32) int32
	x11Free              func(unsafe.Pointer) int32
	x11SendEvent         func(unsafe.Pointer, uintptr, int32, uintptr, unsafe.Pointer) int32
	x11Flush             func(unsafe.Pointer) int32
)

// loadX11Functions loads the Xlib calls used to query and restore the active EWMH window.
func loadX11Functions() bool {
	x11LoadOnce.Do(func() {
		library, err := purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		x11Library = library
		purego.RegisterLibFunc(&x11OpenDisplay, library, "XOpenDisplay")
		purego.RegisterLibFunc(&x11CloseDisplay, library, "XCloseDisplay")
		purego.RegisterLibFunc(&x11DefaultRootWindow, library, "XDefaultRootWindow")
		purego.RegisterLibFunc(&x11InternAtom, library, "XInternAtom")
		purego.RegisterLibFunc(&x11GetWindowProperty, library, "XGetWindowProperty")
		purego.RegisterLibFunc(&x11GetInputFocus, library, "XGetInputFocus")
		purego.RegisterLibFunc(&x11Free, library, "XFree")
		purego.RegisterLibFunc(&x11SendEvent, library, "XSendEvent")
		purego.RegisterLibFunc(&x11Flush, library, "XFlush")
	})
	return x11Library != 0
}

// capturePreviousForegroundWindow reads the active X11 window before the picker is shown.
func capturePreviousForegroundWindow() foregroundTarget {
	display := openX11Display()
	if display == nil {
		return foregroundTarget{}
	}
	defer x11CloseDisplay(display)

	root := x11DefaultRootWindow(display)
	activeWindow, ok := x11WindowProperty(display, root, "_NET_ACTIVE_WINDOW")
	if !ok {
		var revertTo int32
		if x11GetInputFocus(display, &activeWindow, &revertTo) == 0 || activeWindow <= 1 {
			return foregroundTarget{}
		}
	}
	if activeWindow == 0 {
		return foregroundTarget{}
	}
	if processID, ok := x11WindowProperty(display, activeWindow, "_NET_WM_PID"); ok && processID == uintptr(os.Getpid()) {
		return foregroundTarget{}
	}
	return foregroundTarget{handle: activeWindow}
}

// restorePreviousForegroundWindow asks the window manager to activate the window that was previously active.
func restorePreviousForegroundWindow(target foregroundTarget) {
	if target.handle == 0 {
		return
	}
	display := openX11Display()
	if display == nil {
		return
	}
	defer x11CloseDisplay(display)

	root := x11DefaultRootWindow(display)
	activeAtom := x11InternedAtom(display, "_NET_ACTIVE_WINDOW")
	if root == 0 || activeAtom == 0 {
		return
	}

	// XEvent is 192 bytes on supported Linux targets; reserve the whole union for XSendEvent.
	var eventStorage [24]uintptr
	event := (*x11ClientMessageEvent)(unsafe.Pointer(&eventStorage[0]))
	event.eventType = x11ClientMessage
	event.display = display
	event.window = target.handle
	event.messageType = activeAtom
	event.format = 32
	event.data = [5]uintptr{2, 0} // Treat this as a pager activation request.
	mask := uintptr(x11SubstructureNotify | x11SubstructureRedirect)
	x11SendEvent(display, root, 0, mask, unsafe.Pointer(&eventStorage[0]))
	x11Flush(display)
}

func openX11Display() unsafe.Pointer {
	if !loadX11Functions() {
		return nil
	}
	return x11OpenDisplay(nil)
}

func x11WindowProperty(display unsafe.Pointer, window uintptr, name string) (uintptr, bool) {
	atom := x11InternedAtom(display, name)
	if atom == 0 {
		return 0, false
	}
	var actualType uintptr
	var actualFormat int32
	var itemCount, bytesAfter uintptr
	var data unsafe.Pointer
	if x11GetWindowProperty(display, window, atom, 0, 1, 0, 0, &actualType, &actualFormat, &itemCount, &bytesAfter, &data) != 0 {
		return 0, false
	}
	if data == nil {
		return 0, false
	}
	defer x11Free(data)
	if actualFormat != 32 || itemCount == 0 {
		return 0, false
	}
	return *(*uintptr)(data), true
}

func x11InternedAtom(display unsafe.Pointer, name string) uintptr {
	nameBytes := append([]byte(name), 0)
	return x11InternAtom(display, &nameBytes[0], 1)
}
