//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// guiListenPathExists detects any filesystem entry that would occupy the configured socket path.
func guiListenPathExists(endpoint string) (bool, error) {
	if endpoint == "" {
		return false, nil
	}
	path, err := filepath.Abs(endpoint)
	if err != nil {
		return false, fmt.Errorf("resolve listen path: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("inspect listen path: %w", err)
	}
}
