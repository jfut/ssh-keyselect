//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

func openTerminal(_ string) (*terminalSession, error) {
	if usesMSYS2OrCygwinTerminal() {
		// Git Bash and Cygwin provide terminal input through inherited streams, not CONIN$.
		// Use raw console input when available so the picker can edit and echo each key itself.
		inputReader, err := newWindowsTerminalInput(os.Stdin)
		if err != nil {
			return nil, err
		}
		liveEcho, restoreInputMode := enableRawTerminalInput(os.Stdin)
		restoreOutputMode := enableVirtualTerminalOutput(os.Stdout)
		width, _, _ := term.GetSize(int(os.Stdout.Fd()))
		return &terminalSession{
			reader:       inputReader,
			writer:       os.Stdout,
			width:        width,
			echoInput:    !liveEcho,
			liveEcho:     liveEcho,
			inputStopped: make(chan struct{}),
			stopInput:    inputReader.Close,
			close: func() error {
				return errors.Join(restoreInputMode(), restoreOutputMode())
			},
		}, nil
	}

	// Windows exposes console input and output through separate device handles.
	input, err := os.OpenFile("CONIN$", os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	output, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	inputReader, err := newWindowsTerminalInput(input)
	if err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	liveEcho, restoreInputMode := enableRawTerminalInput(input)
	restoreOutputMode := enableVirtualTerminalOutput(output)
	width, _, _ := term.GetSize(int(output.Fd()))
	return &terminalSession{
		reader:       inputReader,
		writer:       output,
		width:        width,
		liveEcho:     liveEcho,
		inputStopped: make(chan struct{}),
		stopInput:    inputReader.Close,
		close: func() error {
			return errors.Join(restoreInputMode(), restoreOutputMode(), input.Close(), output.Close())
		},
	}, nil
}

// Windows terminal readers block until input, so they do not need Unix polling.
func waitForTerminalInput(io.Reader, <-chan struct{}, error) (bool, error) { return false, nil }

// enableVirtualTerminalOutput activates ANSI cursor controls when the writer is a Windows console.
func enableVirtualTerminalOutput(output *os.File) func() error {
	noop := func() error { return nil }
	if output == nil {
		return noop
	}
	handle := windows.Handle(output.Fd())
	var originalMode uint32
	if err := windows.GetConsoleMode(handle, &originalMode); err != nil {
		return noop
	}
	vtMode := originalMode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if vtMode == originalMode {
		return noop
	}
	if err := windows.SetConsoleMode(handle, vtMode); err != nil {
		return noop
	}
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() { restoreErr = windows.SetConsoleMode(handle, originalMode) })
		return restoreErr
	}
}

// enableRawTerminalInput disables the native line editor so the TUI can handle erase and Ctrl+C.
// It returns false when the handle is a pipe or does not support console mode changes.
func enableRawTerminalInput(input *os.File) (bool, func() error) {
	noop := func() error { return nil }
	if input == nil {
		return false, noop
	}

	handle := windows.Handle(input.Fd())
	var originalMode uint32
	if err := windows.GetConsoleMode(handle, &originalMode); err != nil {
		return false, noop
	}
	rawMode := terminalConsoleInputMode(originalMode)
	if rawMode == originalMode {
		return true, noop
	}
	if err := windows.SetConsoleMode(handle, rawMode); err != nil {
		return false, noop
	}

	var once sync.Once
	var restoreErr error
	return true, func() error {
		once.Do(func() {
			restoreErr = windows.SetConsoleMode(handle, originalMode)
		})
		return restoreErr
	}
}

// terminalConsoleInputMode gives the application each key and lets it echo edits explicitly.
func terminalConsoleInputMode(mode uint32) uint32 {
	return (mode &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT)) |
		windows.ENABLE_VIRTUAL_TERMINAL_INPUT
}

func usesMSYS2OrCygwinTerminal() bool {
	return os.Getenv("MSYSTEM") != "" || os.Getenv("CYGWIN") != ""
}
