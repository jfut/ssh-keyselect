//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

var version = "dev"

var commit = "none"

// lockedBuffer collects startup diagnostics safely while the GUI serves requests.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }

func execute(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-V" || args[0] == "version") {
		_, _ = fmt.Fprintf(stdout, "ssh-keyselect-gui %s (%s)\n", version, commit)
		return 0
	}
	if len(args) > 0 && args[0] == "help" {
		return executeGUI([]string{"--help"}, stdout, stderr)
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return executeGUI(args, stdout, stderr)
		}
	}
	return executeGUIDesktop(args, stdout, stderr)
}

// executeGUIDesktop reports startup failures while keeping the GUI attached to its launcher.
func executeGUIDesktop(args []string, stdout, stderr io.Writer) int {
	var diagnostics lockedBuffer
	code := executeGUI(args, stdout, &diagnostics)
	if code != 0 {
		message := strings.TrimSpace(diagnostics.String())
		if message == "" {
			message = "The GUI agent could not start."
		}
		showStartupError(message)
	}
	if stderr != nil {
		_, _ = io.WriteString(stderr, diagnostics.String())
	}
	return code
}
