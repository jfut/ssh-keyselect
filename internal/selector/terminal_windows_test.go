//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConsoleInputModeDisablesNativeEchoLineEditingAndProcessedInput(t *testing.T) {
	const originalMode = windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT
	mode := terminalConsoleInputMode(originalMode)

	if mode != windows.ENABLE_VIRTUAL_TERMINAL_INPUT {
		t.Fatalf("console mode = %#x, want virtual-terminal input only (%#x)", mode, windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	}
}

func TestOpenTerminalUsesInheritedStreamsInMSYS2AndCygwin(t *testing.T) {
	oldStdin, oldStdout := os.Stdin, os.Stdout
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputReader, output, err := os.Pipe()
	if err != nil {
		_ = input.Close()
		_ = inputWriter.Close()
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = input, output
	defer func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
		_ = input.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = output.Close()
	}()

	for _, tt := range []struct {
		name    string
		msystem string
		cygwin  string
	}{
		{name: "MSYS2", msystem: "MINGW64"},
		{name: "Cygwin", cygwin: "tty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MSYSTEM", tt.msystem)
			t.Setenv("CYGWIN", tt.cygwin)
			terminal, err := openTerminal("")
			if err != nil {
				t.Fatal(err)
			}
			if terminal.reader != input || terminal.writer != output {
				t.Fatal("MSYS2/Cygwin terminal should use the inherited standard streams")
			}
			if !terminal.echoInput {
				t.Fatal("pipe-backed input should echo the completed line")
			}
			if err := terminal.Close(); err != nil {
				t.Fatalf("close terminal: %v", err)
			}
		})
	}
	if _, err := inputWriter.Write([]byte("1\n")); err != nil {
		t.Fatalf("closing terminal closed inherited stdin: %v", err)
	}
}
