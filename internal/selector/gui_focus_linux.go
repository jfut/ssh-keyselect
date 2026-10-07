//go:build gui && linux

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

var pickerX11 struct {
	once sync.Once
	ok   bool

	gdkDisplayGetDefault       func() uintptr
	gdkX11DisplayGetType       func() uintptr
	gdkX11DisplayGetXDisplay   func(uintptr) uintptr
	gdkX11DisplayGetUserTime   func(uintptr) uint32
	gdkX11WindowGetXID         func(uintptr) uintptr
	gtkWidgetGetWindow         func(uintptr) uintptr
	gTypeCheckInstanceIsA      func(uintptr, uintptr) int32
	xGetInputFocus             func(uintptr, *uintptr, *int32) int32
	xGetWindowAttributes       func(uintptr, uintptr, *pickerX11WindowAttributes) int32
	xQueryTree                 func(uintptr, uintptr, *uintptr, *uintptr, *uintptr, *uint32) int32
	xInternAtom                func(uintptr, *byte, int32) uintptr
	xGetWindowProperty         func(uintptr, uintptr, uintptr, int64, int64, int32, uintptr, *uintptr, *int32, *uintptr, *uintptr, *uintptr) int32
	xSendEvent                 func(uintptr, uintptr, int32, int64, *[24]uintptr) int32
	xFree                      func(uintptr) int32
	xRaiseWindow               func(uintptr, uintptr) int32
	xSetInputFocus             func(uintptr, uintptr, int32, uintptr) int32
	xSync                      func(uintptr, int32) int32
	gdkX11DisplayErrorTrapPush func(uintptr)
	gdkX11DisplayErrorTrapPop  func(uintptr) int32
	gtkLibrary, gdkLibrary     uintptr
	gobjectLibrary, x11Library uintptr
}

// pickerX11WindowAttributes mirrors Xlib's XWindowAttributes so focus is
// requested only after the window manager has mapped an input-capable window.
type pickerX11WindowAttributes struct {
	x, y, width, height, borderWidth, depth int32
	visual, root                            uintptr
	class                                   int32
	bitGravity, winGravity, backingStore    int32
	backingPlanes, backingPixel             uintptr
	saveUnder                               int32
	colormap                                uintptr
	mapInstalled, mapState                  int32
	allEventMasks, yourEventMask            uintptr
	doNotPropagateMask                      uintptr
	overrideRedirect                        int32
	screen                                  uintptr
}

// pickerX11ClientMessageEvent occupies the first part of Xlib's XEvent union.
type pickerX11ClientMessageEvent struct {
	eventType   int32
	serial      uintptr
	sendEvent   int32
	display     uintptr
	window      uintptr
	messageType uintptr
	format      int32
	data        [5]int64
}

type pickerX11ReturnTarget struct {
	client   uintptr
	topLevel uintptr
	root     uintptr
}

var pickerX11ReturnTargets = struct {
	sync.Mutex
	next uintptr
	byID map[uintptr]pickerX11ReturnTarget
}{byID: make(map[uintptr]pickerX11ReturnTarget)}

// capturePickerReturnWindow remembers the active top-level X11 window for restoration after selection.
func capturePickerReturnWindow() uintptr {
	var active uintptr
	mygo.RunOnMain(func() {
		display, ok := pickerX11Display()
		if !ok {
			return
		}
		var focused uintptr
		var revertTo int32
		if pickerX11.xGetInputFocus(display, &focused, &revertTo) == 0 || focused <= 1 {
			return
		}
		topLevel, root := pickerX11TopLevel(display, focused)
		if topLevel == 0 || root == 0 {
			return
		}
		pickerX11ReturnTargets.Lock()
		for {
			pickerX11ReturnTargets.next++
			if pickerX11ReturnTargets.next != 0 {
				if _, exists := pickerX11ReturnTargets.byID[pickerX11ReturnTargets.next]; !exists {
					break
				}
			}
		}
		active = pickerX11ReturnTargets.next
		pickerX11ReturnTargets.byID[active] = pickerX11ReturnTarget{client: focused, topLevel: topLevel, root: root}
		pickerX11ReturnTargets.Unlock()
	})
	return active
}

// pickerWindowOwnership leaves Linux pickers independent so closing one does not reactivate the GUI owner.
func pickerWindowOwnership(*mygo.Window) (*mygo.Window, bool) { return nil, false }

// acquirePickerNativeFocus asks X11 to focus the picker after GTK presents it.
func acquirePickerNativeFocus(window *mygo.Window) {
	if window == nil || window.IsDestroyed() {
		return
	}
	display, ok := pickerX11Display()
	if !ok {
		return
	}
	gtkWindow := window.NativeHandle()
	if gtkWindow == 0 {
		return
	}
	gdkWindow := pickerX11.gtkWidgetGetWindow(gtkWindow)
	if gdkWindow == 0 {
		return
	}
	xid := pickerX11.gdkX11WindowGetXID(gdkWindow)
	if xid == 0 {
		return
	}
	// The window manager maps newly presented windows asynchronously. Check
	// that the XID is viewable before focusing it, and trap races where its
	// state changes between the check and the focus request.
	pickerX11WithErrorTrap(display, func() {
		var attributes pickerX11WindowAttributes
		if pickerX11.xGetWindowAttributes(display, xid, &attributes) == 0 || !pickerX11CanFocus(attributes) {
			return
		}
		pickerX11.xRaiseWindow(display, xid)
		pickerX11.xSetInputFocus(display, xid, 2, 0) // RevertToParent, CurrentTime.
	})
}

// restorePickerReturnWindow returns focus to the X11 window that started the SSH request.
func capturePickerRestoreTimestamp() uint32 {
	if _, ok := pickerX11Display(); !ok {
		return 0
	}
	return pickerX11.gdkX11DisplayGetUserTime(pickerX11.gdkDisplayGetDefault())
}

func restorePickerReturnWindow(window uintptr, focusTime uint32) {
	if window == 0 {
		return
	}
	pickerX11ReturnTargets.Lock()
	target, ok := pickerX11ReturnTargets.byID[window]
	delete(pickerX11ReturnTargets.byID, window)
	pickerX11ReturnTargets.Unlock()
	if !ok {
		return
	}
	// Let GTK and the window manager finish deactivating the modal picker first;
	// otherwise they can reactivate its owner after this request.
	time.AfterFunc(150*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			display, ok := pickerX11Display()
			if !ok {
				return
			}
			pickerX11WithErrorTrap(display, func() {
				var clientAttributes, topLevelAttributes pickerX11WindowAttributes
				if pickerX11.xGetWindowAttributes(display, target.client, &clientAttributes) == 0 ||
					pickerX11.xGetWindowAttributes(display, target.topLevel, &topLevelAttributes) == 0 ||
					!pickerX11CanFocus(clientAttributes) || !pickerX11CanFocus(topLevelAttributes) {
					return
				}
				pickerX11RequestActivation(display, target, focusTime)
				pickerX11.xRaiseWindow(display, target.topLevel)
				pickerX11.xSetInputFocus(display, target.client, 2, 0) // RevertToParent, CurrentTime.
			})
		})
	})
}

// releasePickerReturnWindow discards a captured focus target when selection ends without restoring it.
func releasePickerReturnWindow(window uintptr) {
	if window == 0 {
		return
	}
	pickerX11ReturnTargets.Lock()
	delete(pickerX11ReturnTargets.byID, window)
	pickerX11ReturnTargets.Unlock()
}

func pickerX11Display() (uintptr, bool) {
	pickerX11.once.Do(pickerX11Load)
	if !pickerX11.ok {
		return 0, false
	}
	display := pickerX11.gdkDisplayGetDefault()
	if display == 0 || pickerX11.gTypeCheckInstanceIsA(display, pickerX11.gdkX11DisplayGetType()) == 0 {
		return 0, false
	}
	xdisplay := pickerX11.gdkX11DisplayGetXDisplay(display)
	return xdisplay, xdisplay != 0
}

func pickerX11Load() {
	var err error
	pickerX11.gdkLibrary, err = purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	pickerX11.gtkLibrary, err = purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	pickerX11.gobjectLibrary, err = purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	pickerX11.x11Library, err = purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	for _, binding := range []struct {
		library uintptr
		name    string
		fn      any
	}{
		{pickerX11.gdkLibrary, "gdk_display_get_default", &pickerX11.gdkDisplayGetDefault},
		{pickerX11.gdkLibrary, "gdk_x11_display_get_type", &pickerX11.gdkX11DisplayGetType},
		{pickerX11.gdkLibrary, "gdk_x11_display_get_xdisplay", &pickerX11.gdkX11DisplayGetXDisplay},
		{pickerX11.gdkLibrary, "gdk_x11_display_get_user_time", &pickerX11.gdkX11DisplayGetUserTime},
		{pickerX11.gdkLibrary, "gdk_x11_window_get_xid", &pickerX11.gdkX11WindowGetXID},
		{pickerX11.gtkLibrary, "gtk_widget_get_window", &pickerX11.gtkWidgetGetWindow},
		{pickerX11.gobjectLibrary, "g_type_check_instance_is_a", &pickerX11.gTypeCheckInstanceIsA},
		{pickerX11.x11Library, "XGetInputFocus", &pickerX11.xGetInputFocus},
		{pickerX11.x11Library, "XGetWindowAttributes", &pickerX11.xGetWindowAttributes},
		{pickerX11.x11Library, "XQueryTree", &pickerX11.xQueryTree},
		{pickerX11.x11Library, "XInternAtom", &pickerX11.xInternAtom},
		{pickerX11.x11Library, "XGetWindowProperty", &pickerX11.xGetWindowProperty},
		{pickerX11.x11Library, "XSendEvent", &pickerX11.xSendEvent},
		{pickerX11.x11Library, "XFree", &pickerX11.xFree},
		{pickerX11.x11Library, "XRaiseWindow", &pickerX11.xRaiseWindow},
		{pickerX11.x11Library, "XSetInputFocus", &pickerX11.xSetInputFocus},
		{pickerX11.x11Library, "XSync", &pickerX11.xSync},
		{pickerX11.gdkLibrary, "gdk_x11_display_error_trap_push", &pickerX11.gdkX11DisplayErrorTrapPush},
		{pickerX11.gdkLibrary, "gdk_x11_display_error_trap_pop", &pickerX11.gdkX11DisplayErrorTrapPop},
	} {
		symbol, symbolErr := purego.Dlsym(binding.library, binding.name)
		if symbolErr != nil {
			return
		}
		purego.RegisterFunc(binding.fn, symbol)
	}
	pickerX11.ok = true
}

// pickerX11TopLevel resolves a focused child surface to its top-level window.
func pickerX11TopLevel(display, window uintptr) (topLevel, rootWindow uintptr) {
	root := uintptr(0)
	for window != 0 {
		queriedRoot, parent, ok := pickerX11QueryTree(display, window)
		if !ok {
			return 0, 0
		}
		root = queriedRoot
		// WM_STATE is set on the managed application window, not its X11 children
		// or the window manager's reparenting frame.
		if pickerX11HasWMState(display, window) {
			return window, root
		}
		if parent == 0 || parent == root {
			return window, root
		}
		window = parent
	}
	return 0, 0
}

func pickerX11QueryTree(display, window uintptr) (root, parent uintptr, ok bool) {
	var children uintptr
	var childCount uint32
	queried := false
	trapped := pickerX11WithErrorTrap(display, func() {
		queried = pickerX11.xQueryTree(display, window, &root, &parent, &children, &childCount) != 0
		if children != 0 {
			pickerX11.xFree(children)
		}
	})
	return root, parent, trapped && queried
}

func pickerX11HasWMState(display, window uintptr) bool {
	atom := pickerX11InternAtom(display, "WM_STATE", true)
	if atom == 0 {
		return false
	}
	var actualType uintptr
	var actualFormat int32
	var itemCount, bytesAfter, property uintptr
	status := int32(-1)
	trapped := pickerX11WithErrorTrap(display, func() {
		status = pickerX11.xGetWindowProperty(display, window, atom, 0, 1, 0, 0,
			&actualType, &actualFormat, &itemCount, &bytesAfter, &property)
		if property != 0 {
			pickerX11.xFree(property)
		}
	})
	return trapped && status == 0 && actualType != 0 && actualFormat == 32 && itemCount > 0
}

func pickerX11InternAtom(display uintptr, name string, onlyIfExists bool) uintptr {
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return 0
	}
	onlyIfExistsFlag := int32(0)
	if onlyIfExists {
		onlyIfExistsFlag = 1
	}
	atom := pickerX11.xInternAtom(display, namePointer, onlyIfExistsFlag)
	runtime.KeepAlive(namePointer)
	return atom
}

func pickerX11RequestActivation(display uintptr, target pickerX11ReturnTarget, focusTime uint32) bool {
	const (
		x11ClientMessage            = 33
		x11SubstructureNotifyMask   = 1 << 19
		x11SubstructureRedirectMask = 1 << 20
		x11ApplicationSource        = 1
	)
	atom := pickerX11InternAtom(display, "_NET_ACTIVE_WINDOW", true)
	if atom == 0 || target.root == 0 {
		return false
	}
	var event [24]uintptr
	message := (*pickerX11ClientMessageEvent)(unsafe.Pointer(&event[0]))
	message.eventType = x11ClientMessage
	message.display = display
	message.window = target.topLevel
	message.messageType = atom
	message.format = 32
	message.data[0] = x11ApplicationSource
	message.data[1] = int64(focusTime)
	mask := int64(x11SubstructureNotifyMask | x11SubstructureRedirectMask)
	sent := pickerX11.xSendEvent(display, target.root, 0, mask, &event) != 0
	runtime.KeepAlive(&event)
	return sent
}

func pickerX11CanFocus(attributes pickerX11WindowAttributes) bool {
	const (
		x11InputOutput = 1
		x11IsViewable  = 2
	)
	return attributes.class == x11InputOutput && attributes.mapState == x11IsViewable
}

// pickerX11WithErrorTrap keeps asynchronous X11 errors from terminating the GUI process.
func pickerX11WithErrorTrap(display uintptr, fn func()) bool {
	gdkDisplay := pickerX11.gdkDisplayGetDefault()
	if gdkDisplay == 0 || pickerX11.gTypeCheckInstanceIsA(gdkDisplay, pickerX11.gdkX11DisplayGetType()) == 0 ||
		pickerX11.gdkX11DisplayGetXDisplay(gdkDisplay) != display {
		return false
	}
	pickerX11.gdkX11DisplayErrorTrapPush(gdkDisplay)
	fn()
	pickerX11.xSync(display, 0)
	return pickerX11.gdkX11DisplayErrorTrapPop(gdkDisplay) == 0
}
