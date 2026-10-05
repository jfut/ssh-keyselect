//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

// DefaultTUIEndpointForMode returns a per-process socket in the private runtime directory.
func DefaultTUIEndpointForMode(_ string, requested transport.Mode) (string, transport.Mode, error) {
	mode, err := ResolveMode("", "", requested)
	if err != nil {
		return "", "", err
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	if !filepath.IsAbs(runtimeDir) {
		return "", "", fmt.Errorf("runtime directory must be absolute: %s", runtimeDir)
	}
	directory := filepath.Join(runtimeDir, "ssh-keyselect")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", fmt.Errorf("create runtime directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", "", fmt.Errorf("inspect runtime directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("runtime path is not a directory: %s", directory)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", "", fmt.Errorf("set runtime directory permissions: %w", err)
	}
	return filepath.Join(directory, "agent."+strconv.Itoa(os.Getpid())), mode, nil
}

// ResolveMode selects the transport mode for an endpoint on Unix platforms.
func ResolveMode(_ string, _ string, requested transport.Mode) (transport.Mode, error) {
	mode, err := transport.ParseMode(string(requested))
	if err != nil {
		return "", err
	}
	if mode == transport.Auto || mode == transport.WSL1 {
		return transport.Unix, nil
	}
	if mode != transport.Unix {
		return "", fmt.Errorf("listen mode %q is supported only on Windows", mode)
	}
	return mode, nil
}
