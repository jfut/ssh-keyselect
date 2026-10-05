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

// guiDiagnostics forwards running logs and retains a bounded tail for startup error dialogs.
type guiDiagnostics struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	output io.Writer
}

const guiDiagnosticLimit = 64 * 1024

func (b *guiDiagnostics) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	original := data
	if len(data) >= guiDiagnosticLimit {
		b.buf.Reset()
		data = data[len(data)-guiDiagnosticLimit:]
	} else if discard := b.buf.Len() + len(data) - guiDiagnosticLimit; discard > 0 {
		b.buf.Next(discard)
	}
	_, _ = b.buf.Write(data)
	if b.output != nil {
		return b.output.Write(original)
	}
	return n, nil
}

func (b *guiDiagnostics) String() string {
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
	diagnostics := guiDiagnostics{output: stderr}
	code := executeGUI(args, stdout, &diagnostics)
	if code != 0 {
		message := strings.TrimSpace(diagnostics.String())
		if message == "" {
			message = "The GUI agent could not start."
		}
		showStartupError(message)
	}
	return code
}
