//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// guiListenPathExists detects entries that cannot be reclaimed as stale Unix sockets.
func guiListenPathExists(endpoint string) (bool, error) {
	if endpoint == "" {
		return false, nil
	}
	path, err := filepath.Abs(endpoint)
	if err != nil {
		return false, fmt.Errorf("resolve listen path: %w", err)
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return true, nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Uid) != uint64(os.Geteuid()) {
			return true, nil
		}
		conn, dialErr := net.DialTimeout("unix", path, 300*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return true, nil
		}
		if errors.Is(dialErr, syscall.ECONNREFUSED) || errors.Is(dialErr, syscall.ENOENT) {
			// Let the listener verify ownership and identity before removing a stale socket.
			return false, nil
		}
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("inspect listen path: %w", err)
	}
}
