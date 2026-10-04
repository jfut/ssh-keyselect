// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

//go:build windows

// Package systemtray manages the Windows notification-area icon used by the GUI agent.
package systemtray

import (
	"bytes"
	"fmt"
	"image/png"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/jfut/ssh-keyselect/internal/branding"
	"golang.org/x/sys/windows"
)

const (
	wmClose           = 0x0010
	wmNull            = 0x0000
	wmDestroy         = 0x0002
	wmCommand         = 0x0111
	wmContextMenu     = 0x007B
	wmApp             = 0x8000
	wmTrayCallback    = wmApp + 1
	wmLButtonDblClick = 0x0203
	wmRButtonUp       = 0x0205

	wsPopup        = 0x80000000
	wsExToolWin    = 0x00000080
	csDoubleClicks = 0x0008

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	mfString = 0x00000000

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	dibRGBColors = 0
	biRGB        = 0
	trayIconSize = 32
	maxTraySlots = 1024

	trayMenuShow    = 1001
	trayMenuRefresh = 1002
	trayMenuQuit    = 1003
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	gdi32            = windows.NewLazySystemDLL("gdi32.dll")
	getModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	registerClassExW = user32.NewProc("RegisterClassExW")
	createWindowExW  = user32.NewProc("CreateWindowExW")
	defWindowProcW   = user32.NewProc("DefWindowProcW")
	getMessageW      = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessageW = user32.NewProc("DispatchMessageW")
	postMessageW     = user32.NewProc("PostMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	createPopupMenu  = user32.NewProc("CreatePopupMenu")
	appendMenuW      = user32.NewProc("AppendMenuW")
	trackPopupMenu   = user32.NewProc("TrackPopupMenu")
	destroyMenu      = user32.NewProc("DestroyMenu")
	getCursorPos     = user32.NewProc("GetCursorPos")
	setForeground    = user32.NewProc("SetForegroundWindow")
	destroyWindow    = user32.NewProc("DestroyWindow")
	unregisterClassW = user32.NewProc("UnregisterClassW")
	createIconDirect = user32.NewProc("CreateIconIndirect")
	destroyIcon      = user32.NewProc("DestroyIcon")
	shellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	createDIBSection = gdi32.NewProc("CreateDIBSection")
	createBitmap     = gdi32.NewProc("CreateBitmap")
	deleteObject     = gdi32.NewProc("DeleteObject")
	trayWindowClass  = windows.StringToUTF16Ptr("ssh-keyselect-system-tray")
	trayWindowTitle  = windows.StringToUTF16Ptr(branding.Name)
)

type windowClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	classExtra int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSmall  windows.Handle
}

type message struct {
	window  windows.Handle
	id      uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   winPoint
	private uint32
}

// winPoint aligns POINT to match the native MSG structure on 32-bit and 64-bit Windows.
type winPoint struct {
	_ [0]uint64
	x int32
	y int32
}

// notifyIconData mirrors the full Shell_NotifyIcon structure used to add and remove the icon.
type notifyIconData struct {
	Size             uint32
	Window           windows.Handle
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             [16]byte
	BalloonIcon      windows.Handle
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type iconInfo struct {
	IsIcon      int32
	XHotspot    uint32
	YHotspot    uint32
	MaskBitmap  windows.Handle
	ColorBitmap windows.Handle
}

// Supported reports whether this build can create an operating-system tray icon.
func Supported() bool { return true }

// Callbacks connects tray menu choices to the GUI actions.
type Callbacks struct {
	Show    func()
	Quit    func()
	Refresh func()
	IconPNG []byte
}

// Start creates the notification-area icon and returns a function that updates its endpoint label.
func Start(callbacks Callbacks, endpoint string) (func() error, func(string) error, error) {
	ready := make(chan startResult, 1)
	done := make(chan error, 1)
	go runTray(ready, done, callbacks, endpoint)
	started := <-ready
	if started.err != nil {
		return nil, nil, started.err
	}
	var once sync.Once
	var stopErr error
	cleanup := func() error {
		once.Do(func() {
			result, _, callErr := postMessageW.Call(started.window, wmClose, 0, 0)
			if result == 0 {
				postErr := winCallError("post close message to tray window", callErr)
				select {
				case stopErr = <-done:
				default:
					stopErr = postErr
				}
			} else {
				stopErr = <-done
			}
		})
		return stopErr
	}
	updateTooltip := func(endpoint string) error {
		data := notifyIconData{
			Size:   uint32(unsafe.Sizeof(notifyIconData{})),
			Window: windows.Handle(started.window),
			ID:     started.slot,
			Flags:  nifTip,
		}
		setTrayTooltip(&data, trayTooltip(endpoint))
		return notifyTrayIcon(nimModify, &data)
	}
	return cleanup, updateTooltip, nil
}

type startResult struct {
	window uintptr
	slot   uint32
	err    error
}

func runTray(ready chan<- startResult, done chan<- error, callbacks Callbacks, endpoint string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	traySlot, slotHandle, err := acquireTraySlot()
	if err != nil {
		ready <- startResult{err: err}
		done <- nil
		return
	}
	defer func() { _ = windows.CloseHandle(slotHandle) }()

	instanceValue, _, callErr := getModuleHandleW.Call(0)
	if instanceValue == 0 {
		ready <- startResult{err: winCallError("get application module handle", callErr)}
		done <- nil
		return
	}
	instance := windows.Handle(instanceValue)
	windowProc := windows.NewCallback(func(hwnd uintptr, id uint32, wParam, lParam uintptr) uintptr {
		switch id {
		case wmTrayCallback:
			switch lParam {
			case wmLButtonDblClick:
				if callbacks.Show != nil {
					callbacks.Show()
				}
			case wmRButtonUp, wmContextMenu:
				showTrayMenu(hwnd, callbacks)
			}
			return 0
		case wmCommand:
			switch wParam & 0xFFFF {
			case trayMenuShow:
				if callbacks.Show != nil {
					callbacks.Show()
				}
			case trayMenuRefresh:
				if callbacks.Refresh != nil {
					callbacks.Refresh()
				}
			case trayMenuQuit:
				if callbacks.Quit != nil {
					callbacks.Quit()
				}
			}
			return 0
		case wmClose:
			deleteTrayIcon(hwnd, traySlot)
			_, _, _ = destroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			_, _, _ = postQuitMessage.Call(0)
			return 0
		default:
			result, _, _ := defWindowProcW.Call(hwnd, uintptr(id), wParam, lParam)
			return result
		}
	})
	class := windowClassEx{
		size:      uint32(unsafe.Sizeof(windowClassEx{})),
		style:     csDoubleClicks,
		wndProc:   windowProc,
		instance:  instance,
		className: trayWindowClass,
	}
	atom, _, callErr := registerClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		ready <- startResult{err: winCallError("register tray window class", callErr)}
		done <- nil
		return
	}
	window, _, callErr := createWindowExW.Call(
		wsExToolWin,
		uintptr(unsafe.Pointer(trayWindowClass)),
		uintptr(unsafe.Pointer(trayWindowTitle)),
		wsPopup,
		0, 0, 0, 0,
		0, 0,
		uintptr(instance), 0,
	)
	if window == 0 {
		_, _, _ = unregisterClassW.Call(uintptr(unsafe.Pointer(trayWindowClass)), uintptr(instance))
		ready <- startResult{err: winCallError("create tray window", callErr)}
		done <- nil
		return
	}
	icon, iconErr := createTrayAppIcon(callbacks.IconPNG)
	if iconErr != nil {
		_, _, _ = destroyWindow.Call(window)
		_, _, _ = unregisterClassW.Call(uintptr(unsafe.Pointer(trayWindowClass)), uintptr(instance))
		ready <- startResult{err: iconErr}
		done <- nil
		return
	}
	defer func() { _, _, _ = destroyIcon.Call(uintptr(icon)) }()
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Window:          windows.Handle(window),
		ID:              traySlot,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            icon,
	}
	setTrayTooltip(&data, trayTooltip(endpoint))
	if err := notifyTrayIcon(nimAdd, &data); err != nil {
		_, _, _ = destroyWindow.Call(window)
		_, _, _ = unregisterClassW.Call(uintptr(unsafe.Pointer(trayWindowClass)), uintptr(instance))
		ready <- startResult{err: err}
		done <- nil
		return
	}
	ready <- startResult{window: window, slot: traySlot}

	var msg message
	for {
		result, _, callErr := getMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(result) {
		case -1:
			deleteTrayIcon(window, traySlot)
			_, _, _ = destroyWindow.Call(window)
			_, _, _ = unregisterClassW.Call(uintptr(unsafe.Pointer(trayWindowClass)), uintptr(instance))
			done <- winCallError("read tray window message", callErr)
			return
		case 0:
			_, _, _ = unregisterClassW.Call(uintptr(unsafe.Pointer(trayWindowClass)), uintptr(instance))
			done <- nil
			return
		default:
			_, _, _ = translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			_, _, _ = dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
}

// showTrayMenu displays the Windows tray menu and runs the selected GUI action.
func showTrayMenu(window uintptr, callbacks Callbacks) {
	menu, _, callErr := createPopupMenu.Call()
	if menu == 0 {
		_ = winCallError("create tray context menu", callErr)
		return
	}
	defer func() { _, _, _ = destroyMenu.Call(menu) }()
	if err := appendMenuItem(menu, trayMenuShow, "Show"); err != nil {
		return
	}
	if err := appendMenuItem(menu, trayMenuRefresh, "Refresh Keys"); err != nil {
		return
	}
	if err := appendMenuItem(menu, trayMenuQuit, "Quit"); err != nil {
		return
	}
	var position winPoint
	if result, _, callErr := getCursorPos.Call(uintptr(unsafe.Pointer(&position))); result == 0 {
		_ = winCallError("get tray menu position", callErr)
		return
	}
	_, _, _ = setForeground.Call(window)
	command, _, _ := trackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(position.x), uintptr(position.y), 0, window, 0)
	_, _, _ = postMessageW.Call(window, wmNull, 0, 0)
	switch command {
	case trayMenuShow:
		if callbacks.Show != nil {
			callbacks.Show()
		}
	case trayMenuRefresh:
		if callbacks.Refresh != nil {
			callbacks.Refresh()
		}
	case trayMenuQuit:
		if callbacks.Quit != nil {
			callbacks.Quit()
		}
	}
}

// createTrayAppIcon converts the embedded PNG into a Windows HICON for the notification area.
func createTrayAppIcon(iconPNG []byte) (windows.Handle, error) {
	decoded, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return 0, fmt.Errorf("decode application icon: %w", err)
	}
	info := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       trayIconSize,
			Height:      -trayIconSize,
			Planes:      1,
			BitCount:    32,
			Compression: biRGB,
		},
	}
	var bits uintptr
	colorBitmap, _, callErr := createDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	runtime.KeepAlive(info)
	if colorBitmap == 0 || bits == 0 {
		return 0, winCallError("create tray icon bitmap", callErr)
	}
	defer func() { _, _, _ = deleteObject.Call(colorBitmap) }()

	pixels := unsafe.Slice((*byte)(unsafe.Pointer(bits)), trayIconSize*trayIconSize*4)
	bounds := decoded.Bounds()
	for y := range trayIconSize {
		for x := range trayIconSize {
			sx := bounds.Min.X + x*bounds.Dx()/trayIconSize
			sy := bounds.Min.Y + y*bounds.Dy()/trayIconSize
			red, green, blue, alpha := decoded.At(sx, sy).RGBA()
			index := (y*trayIconSize + x) * 4
			pixels[index+0] = byte(blue >> 8)
			pixels[index+1] = byte(green >> 8)
			pixels[index+2] = byte(red >> 8)
			pixels[index+3] = byte(alpha >> 8)
		}
	}

	// The monochrome mask uses bottom-up rows and marks only fully transparent pixels.
	const maskStride = (trayIconSize + 7) / 8
	mask := make([]byte, maskStride*trayIconSize)
	for y := range trayIconSize {
		for x := range trayIconSize {
			alpha := pixels[(y*trayIconSize+x)*4+3]
			if alpha == 0 {
				row := trayIconSize - 1 - y
				mask[row*maskStride+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	maskBitmap, _, callErr := createBitmap.Call(
		trayIconSize,
		trayIconSize,
		1,
		1,
		uintptr(unsafe.Pointer(&mask[0])),
	)
	runtime.KeepAlive(mask)
	if maskBitmap == 0 {
		return 0, winCallError("create tray icon mask", callErr)
	}
	defer func() { _, _, _ = deleteObject.Call(maskBitmap) }()

	iconData := iconInfo{
		IsIcon:      1,
		MaskBitmap:  windows.Handle(maskBitmap),
		ColorBitmap: windows.Handle(colorBitmap),
	}
	icon, _, callErr := createIconDirect.Call(uintptr(unsafe.Pointer(&iconData)))
	runtime.KeepAlive(iconData)
	if icon == 0 {
		return 0, winCallError("create tray application icon", callErr)
	}
	return windows.Handle(icon), nil
}

func appendMenuItem(menu, command uintptr, title string) error {
	text := windows.StringToUTF16(title)
	result, _, callErr := appendMenuW.Call(menu, mfString, command, uintptr(unsafe.Pointer(&text[0])))
	runtime.KeepAlive(text)
	if result == 0 {
		return winCallError("add tray menu item", callErr)
	}
	return nil
}

func notifyTrayIcon(action uintptr, data *notifyIconData) error {
	result, _, callErr := shellNotifyIconW.Call(action, uintptr(unsafe.Pointer(data)))
	runtime.KeepAlive(data)
	if result == 0 {
		return winCallError("update Windows notification-area icon", callErr)
	}
	return nil
}

// setTrayTooltip writes the endpoint label into the fixed-size Windows notification-icon field.
func setTrayTooltip(data *notifyIconData, tooltip string) {
	data.Flags |= nifTip
	encoded := utf16.Encode([]rune(tooltip))
	maxUnits := len(data.Tip) - 1
	if len(encoded) > maxUnits {
		encoded = encoded[:maxUnits]
		last := encoded[len(encoded)-1]
		if last >= 0xD800 && last <= 0xDBFF {
			encoded = encoded[:len(encoded)-1]
		}
	}
	copy(data.Tip[:], encoded)
	data.Tip[len(encoded)] = 0
}

// acquireTraySlot reserves the lowest free per-user slot for this process.
// Its named mutex remains alive while the process owns a notification icon.
func acquireTraySlot() (uint32, windows.Handle, error) {
	for slot := uint32(1); slot <= maxTraySlots; slot++ {
		name := windows.StringToUTF16Ptr(fmt.Sprintf(`Local\ssh-keyselect-system-tray-%d`, slot))
		handle, err := windows.CreateMutex(nil, false, name)
		if err == windows.ERROR_ALREADY_EXISTS {
			_ = windows.CloseHandle(handle)
			continue
		}
		if err != nil {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
			return 0, 0, fmt.Errorf("reserve notification-area icon slot %d: %w", slot, err)
		}
		return slot, handle, nil
	}
	return 0, 0, fmt.Errorf("all %d notification-area icon slots are in use", maxTraySlots)
}

func deleteTrayIcon(window uintptr, slot uint32) {
	data := notifyIconData{
		Size:   uint32(unsafe.Sizeof(notifyIconData{})),
		Window: windows.Handle(window), ID: slot,
	}
	_ = notifyTrayIcon(nimDelete, &data)
}

func winCallError(operation string, callErr error) error {
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("%s: %w", operation, callErr)
	}
	return fmt.Errorf("%s failed", operation)
}
