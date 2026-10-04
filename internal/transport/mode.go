// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package transport defines supported SSH agent endpoint transports.
package transport

import (
	"fmt"
	"strings"
)

// Mode selects the transport used for an agent endpoint.
type Mode string

const (
	Auto      Mode = "auto"
	Unix      Mode = "unix"
	Cygwin    Mode = "cygwin"
	WSL1      Mode = "wsl1"
	NamedPipe Mode = "named-pipe"
)

// ParseMode normalizes and validates a configured endpoint transport.
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		return Auto, nil
	}
	switch mode {
	case Auto, Unix, Cygwin, WSL1, NamedPipe:
		return mode, nil
	default:
		return "", fmt.Errorf("mode must be auto, unix, cygwin, wsl1, or named-pipe")
	}
}
