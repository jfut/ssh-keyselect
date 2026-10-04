//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"os"

	"golang.org/x/term"
)

func openTerminal(path string) (*terminalSession, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	width, _, _ := term.GetSize(int(file.Fd()))
	return &terminalSession{
		reader:   file,
		writer:   file,
		width:    width,
		liveEcho: true,
		close: func() error {
			return errors.Join(term.Restore(int(file.Fd()), state), file.Close())
		},
	}, nil
}
