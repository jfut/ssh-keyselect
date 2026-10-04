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
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(namedPipePrefix))
}

// IsGitBashPath reports whether path uses a Git Bash/MSYS2 drive-mount format.
func IsGitBashPath(path string) bool { return isGitBashPath(path) }

// IsWSL1DriveMountPath reports whether path uses a WSL1 Windows-drive mount.
func IsWSL1DriveMountPath(path string) bool { return isWSL1DriveMountPath(path) }

// GitBashSocketPath returns a short per-user endpoint in the same directory used by Git Bash ssh-agent.
func GitBashSocketPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	nativePath := filepath.Join(home, ".ssh", "agent", name)
	if len(nativePath) >= 108 {
		return "", fmt.Errorf("Windows Unix socket path is too long: %s", nativePath)
	}
	return ToGitBashPath(nativePath)
}

// CompatibleSocketPath returns a socket path in the mount style of the upstream shell.
func CompatibleSocketPath(upstream, name string) (string, error) {
	if isWSL1DriveMountPath(upstream) {
		nativePath, err := nativeAgentSocketPath(name)
		if err != nil {
			return "", err
		}
		gitBashPath, err := ToGitBashPath(nativePath)
		if err != nil {
			return "", err
		}
		return "/mnt" + gitBashPath, nil
	}
	return GitBashSocketPath(name)
}

// NativeSocketPath converts a Git Bash path to the native path expected by Winsock.
func NativeSocketPath(path string) (string, error) {
	if isDriveMountPath(path) {
		return filepath.Clean(string(path[1]) + ":" + filepath.FromSlash(path[2:])), nil
	}
	if isWSL1DriveMountPath(path) {
		return filepath.Clean(string(path[5]) + ":" + filepath.FromSlash(path[6:])), nil
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

func isGitBashPath(path string) bool {
	return isDriveMountPath(path) || path == "/tmp" || strings.HasPrefix(path, "/tmp/")
}

func isWSL1DriveMountPath(path string) bool {
	return len(path) >= 8 && strings.HasPrefix(path, "/mnt/") && isASCIIAlpha(path[5]) && path[6] == '/'
}

func nativeAgentSocketPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	nativePath := filepath.Join(home, ".ssh", "agent", name)
	if len(nativePath) >= 108 {
		return "", fmt.Errorf("Windows Unix socket path is too long: %s", nativePath)
	}
	return nativePath, nil
}

func isDriveMountPath(path string) bool {
	return len(path) >= 3 && path[0] == '/' && isASCIIAlpha(path[1]) && path[2] == '/'
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
