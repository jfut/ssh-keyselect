//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package winpath translates between Windows socket paths and POSIX shell paths.
package winpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const namedPipePrefix = `\\.\pipe\`

// IsNamedPipe reports whether path names a local Windows named pipe.
func IsNamedPipe(path string) bool {
	return len(path) >= len(namedPipePrefix) && strings.EqualFold(path[:len(namedPipePrefix)], namedPipePrefix)
}

// IsGitBashPath reports whether path uses a Git Bash/MSYS2 drive-mount format.
func IsGitBashPath(path string) bool {
	return isDriveMountPath(path) || path == "/tmp" || strings.HasPrefix(path, "/tmp/")
}

// IsWSL1DriveMountPath reports whether path uses a WSL1 Windows-drive mount.
func IsWSL1DriveMountPath(path string) bool {
	return len(path) >= 8 && strings.HasPrefix(path, "/mnt/") && isASCIIAlpha(path[5]) && path[6] == '/'
}

// GitBashSocketPath returns a short per-user endpoint in the same directory used by Git Bash ssh-agent.
func GitBashSocketPath(name string) (string, error) {
	nativePath, err := nativeAgentSocketPath(name)
	if err != nil {
		return "", err
	}
	return ToGitBashPath(nativePath)
}

// CompatibleSocketPath returns a socket path in the mount style of the upstream shell.
func CompatibleSocketPath(upstream, name string) (string, error) {
	path, err := GitBashSocketPath(name)
	if err != nil {
		return "", err
	}
	if IsWSL1DriveMountPath(upstream) {
		path = "/mnt" + path
	}
	return path, nil
}

// NativeSocketPath converts a Git Bash path to the native path expected by Winsock.
func NativeSocketPath(path string) (string, error) {
	if isDriveMountPath(path) {
		return filepath.Clean(strings.ToUpper(path[1:2]) + ":" + filepath.FromSlash(path[2:])), nil
	}
	if IsWSL1DriveMountPath(path) {
		return filepath.Clean(strings.ToUpper(path[5:6]) + ":" + filepath.FromSlash(path[6:])), nil
	}
	// Cygwin drive mounts must resolve identically in display, dialing, and endpoint comparisons.
	if len(path) >= 11 && strings.EqualFold(path[:10], "/cygdrive/") && isASCIIAlpha(path[10]) &&
		(len(path) == 11 || path[11] == '/') {
		return filepath.Clean(strings.ToUpper(path[10:11]) + ":" + filepath.FromSlash(path[11:])), nil
	}
	if path == "/tmp" || strings.HasPrefix(path, "/tmp/") {
		tempDir := os.Getenv("TEMP")
		if tempDir == "" {
			tempDir = os.TempDir()
		}
		return filepath.Join(tempDir, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, "/tmp"), "/"))), nil
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("unsupported Git Bash socket path %q", path)
	}
	return filepath.Abs(path)
}

// ToGitBashPath converts a native drive path to the /c/... form accepted by Git Bash OpenSSH.
func ToGitBashPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve socket path: %w", err)
	}
	volume := filepath.VolumeName(absPath)
	if len(volume) != 2 || volume[1] != ':' {
		return "", fmt.Errorf("socket path must be on a drive: %s", absPath)
	}
	rest := strings.TrimPrefix(absPath[len(volume):], string(filepath.Separator))
	return "/" + strings.ToLower(volume[:1]) + "/" + filepath.ToSlash(rest), nil
}

func nativeAgentSocketPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	nativePath := filepath.Join(home, ".ssh", "agent", name)
	if len(nativePath) >= 108 {
		return "", fmt.Errorf("windows Unix socket path is too long: %s", nativePath)
	}
	return nativePath, nil
}

func isDriveMountPath(path string) bool {
	return len(path) >= 3 && path[0] == '/' && isASCIIAlpha(path[1]) && path[2] == '/'
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
