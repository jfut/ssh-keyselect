//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"fmt"
	"os"
)

// showStartupError reports startup failures on platforms with a terminal launch.
func showStartupError(message string) { fmt.Fprintln(os.Stderr, message) }
