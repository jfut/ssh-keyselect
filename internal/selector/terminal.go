// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"io"
	"sync"
)

// terminalSession is the shared I/O contract between the TUI and each platform backend.
type terminalSession struct {
	reader io.Reader
	writer io.Writer
	width  int
	// inputStopped interrupts readiness polling without closing the output side.
	inputStopped chan struct{}
	// waitInput waits for platform-specific readiness after a nonblocking read.
	waitInput func(error) (bool, error)
	// echoInput prints a completed line when the inherited stream has no live echo.
	echoInput bool
	// liveEcho enables the key-by-key picker when the terminal provides raw input.
	liveEcho      bool
	stopInput     func() error
	inputStopOnce sync.Once
	inputStopErr  error
	close         func() error
	// closeOnce is only used by Close in this file.
	closeOnce sync.Once
	// closeErr is only read by Close in this file.
	closeErr error
}

// StopInput interrupts pending reads while leaving output and terminal modes
// available until the picker has finished writing its final line.
func (s *terminalSession) StopInput() error {
	s.inputStopOnce.Do(func() {
		if s.inputStopped != nil {
			close(s.inputStopped)
		}
		if s.stopInput != nil {
			s.inputStopErr = s.stopInput()
		}
	})
	return s.inputStopErr
}

func (s *terminalSession) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.StopInput()
		if s.close != nil {
			s.closeErr = errors.Join(s.closeErr, s.close())
		}
	})
	return s.closeErr
}
