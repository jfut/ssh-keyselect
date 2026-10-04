// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"io"
	"sync"
)

// terminalSession is the shared I/O contract between the TUI and each platform backend.
type terminalSession struct {
	reader io.Reader
	writer io.Writer
	width  int
	// echoInput prints a completed line when the inherited stream has no live echo.
	echoInput bool
	// liveEcho lets the input reader echo printable bytes and erase keys itself.
	liveEcho bool
	close    func() error
	// closeOnce is only used by Close in this file.
	closeOnce sync.Once
	// closeErr is only read by Close in this file.
	closeErr error
}

func (s *terminalSession) Close() error {
	s.closeOnce.Do(func() {
		if s.close != nil {
			s.closeErr = s.close()
		}
	})
	return s.closeErr
}
