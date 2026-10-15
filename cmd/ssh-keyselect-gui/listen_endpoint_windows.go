//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
	"github.com/jfut/ssh-keyselect/internal/winsocket"
	"golang.org/x/sys/windows"
)

// guiListenPathExists checks filesystem endpoints and leaves named pipes to the pipe listener.
func guiListenPathExists(endpoint string) (bool, error) {
	if endpoint == "" || winpath.IsNamedPipe(endpoint) {
		return false, nil
	}
	path, err := winpath.NativeSocketPath(endpoint)
	if err != nil {
		return false, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket != 0 {
		if err := winsocket.CheckCurrentUserFileOwner(path); err != nil {
			return true, nil
		}
		conn, dialErr := net.DialTimeout("unix", path, 300*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return true, nil
		}
		return !guiWindowsSocketProbeFoundStale(dialErr), nil
	}

	mode, modeErr := winsocket.DetectMode(endpoint)
	if modeErr != nil || mode != transport.Cygwin {
		// Non-socket files and unknown compatibility metadata still occupy the path.
		return true, nil
	}
	if err := winsocket.CheckCurrentUserFileOwner(path); err != nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	conn, dialErr := winsocket.Dial(ctx, endpoint)
	cancel()
	if dialErr == nil {
		_ = conn.Close()
		return true, nil
	}
	return !guiWindowsSocketProbeFoundStale(dialErr), nil
}

// guiWindowsSocketProbeFoundStale accepts only errors proving that no listener remains.
func guiWindowsSocketProbeFoundStale(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, os.ErrNotExist)
}
