//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"golang.org/x/sys/unix"
)

// DefaultTUIEndpointForMode returns a per-process socket in the private runtime directory.
func DefaultTUIEndpointForMode(_ string, requested transport.Mode) (string, transport.Mode, error) {
	mode, err := ResolveMode("", "", requested)
	if err != nil {
		return "", "", err
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	directoryName := "ssh-keyselect"
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
		// The temporary directory is shared across users, unlike XDG_RUNTIME_DIR.
		directoryName += "-" + strconv.Itoa(os.Geteuid())
	} else if err := checkRuntimeDirectoryOwner(runtimeDir); err != nil {
		return "", "", err
	}
	if !filepath.IsAbs(runtimeDir) {
		return "", "", fmt.Errorf("runtime directory must be absolute: %s", runtimeDir)
	}
	directory := filepath.Join(runtimeDir, directoryName)
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", "", fmt.Errorf("create runtime directory: %w", err)
	}
	// Open the directory itself without following a symlink, and change permissions
	// through that descriptor only after confirming it belongs to the current user.
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", "", fmt.Errorf("inspect runtime directory: %w", err)
	}
	file := os.NewFile(uintptr(fd), directory)
	defer func() { _ = file.Close() }()
	if err := checkRuntimeDirectoryFileOwner(file); err != nil {
		return "", "", err
	}
	if err := file.Chmod(0700); err != nil {
		return "", "", fmt.Errorf("set runtime directory permissions: %w", err)
	}
	return filepath.Join(directory, "agent."+strconv.Itoa(os.Getpid())), mode, nil
}

func checkRuntimeDirectoryOwner(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("runtime directory must be absolute: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open runtime directory: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	return checkRuntimeDirectoryFileOwner(file)
}

// Check ownership before chmod so a privileged launch cannot modify another user's directory.
func checkRuntimeDirectoryFileOwner(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Uid) != uint64(os.Geteuid()) {
		return fmt.Errorf("runtime directory is not owned by the current user: %s", file.Name())
	}
	return nil
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
