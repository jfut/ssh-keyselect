//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

const randomPipePrefix = `\\.\pipe\ssh-keyselect.`

// DefaultTUIEndpointForMode returns a unique endpoint for the selected client transport.
func DefaultTUIEndpointForMode(upstream string, requested transport.Mode) (string, transport.Mode, error) {
	mode, err := ResolveMode("", upstream, requested)
	if err != nil {
		return "", "", err
	}
	token := rand.Text()
	name := "s.ssh-keyselect." + token
	switch mode {
	case transport.NamedPipe:
		return randomPipePrefix + token, mode, nil
	case transport.Cygwin, transport.Unix:
		path, err := winpath.GitBashSocketPath(name)
		return path, mode, err
	case transport.WSL1:
		path, err := winpath.CompatibleSocketPath(upstream, name)
		return path, mode, err
	default:
		return "", "", fmt.Errorf("unsupported listen mode %q", mode)
	}
}

// ResolveMode selects a frontend transport using an explicit mode or the invoking shell's paths.
func ResolveMode(listenPath, upstream string, requested transport.Mode) (transport.Mode, error) {
	mode, err := transport.ParseMode(string(requested))
	if err != nil {
		return "", err
	}
	if mode != transport.Auto {
		return mode, nil
	}
	if winpath.IsNamedPipe(listenPath) {
		return transport.NamedPipe, nil
	}
	if winpath.IsWSL1DriveMountPath(listenPath) || winpath.IsWSL1DriveMountPath(upstream) {
		return transport.WSL1, nil
	}
	if os.Getenv("MSYSTEM") != "" || os.Getenv("CYGWIN") != "" || winpath.IsGitBashPath(listenPath) || listenPath == "" && winpath.IsGitBashPath(upstream) {
		return transport.Cygwin, nil
	}
	if listenPath == "" {
		return transport.NamedPipe, nil
	}
	return transport.Unix, nil
}
