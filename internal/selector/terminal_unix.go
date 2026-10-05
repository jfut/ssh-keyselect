//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/term"
)

func openTerminal(path string) (*terminalSession, error) {
	// Nonblocking descriptors let Go's poller interrupt a pending terminal read on Close.
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	raw, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	var fd int
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		_ = file.Close()
		return nil, err
	}
	// File.Fd would switch the descriptor back to blocking mode.
	state, err := term.MakeRaw(fd)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	width, _, _ := term.GetSize(fd)
	return &terminalSession{
		reader:   file,
		writer:   file,
		width:    width,
		liveEcho: true,
		// Expire only input polling; the shared descriptor must still accept output.
		stopInput: func() error { return file.SetReadDeadline(time.Now()) },
		close: func() error {
			return errors.Join(term.Restore(fd, state), file.Close())
		},
	}, nil
}
