//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func openTerminal(path string) (*terminalSession, error) {
	// Nonblocking descriptors let readiness polling interrupt terminal reads on cancellation.
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
	inputStopped := make(chan struct{})
	return &terminalSession{
		reader:       file,
		writer:       file,
		width:        width,
		liveEcho:     true,
		inputStopped: inputStopped,
		waitInput: func(readErr error) (bool, error) {
			return waitForTerminalInput(fd, inputStopped, readErr)
		},
		// Expire only input polling; the shared descriptor must still accept output.
		stopInput: func() error { return file.SetReadDeadline(time.Now()) },
		close: func() error {
			return errors.Join(term.Restore(fd, state), file.Close())
		},
	}, nil
}

// waitForTerminalInput handles EAGAIN from nonblocking terminal reads with poll(2).
func waitForTerminalInput(fd int, stopped <-chan struct{}, readErr error) (bool, error) {
	if !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EWOULDBLOCK) {
		return false, nil
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		select {
		case <-stopped:
			return true, os.ErrClosed
		default:
		}
		ready, err := unix.Poll(fds, 100)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return true, err
		}
		select {
		case <-stopped:
			return true, os.ErrClosed
		default:
		}
		if ready > 0 {
			return true, nil
		}
	}
}
