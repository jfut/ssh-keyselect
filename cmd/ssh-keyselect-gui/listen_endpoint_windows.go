//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"os"

	"github.com/jfut/ssh-keyselect/internal/winpath"
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
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}
