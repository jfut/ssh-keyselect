//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import "os/exec"

// sshExitCode preserves the OpenSSH process exit status on Windows.
func sshExitCode(exitErr *exec.ExitError) int { return exitErr.ExitCode() }
