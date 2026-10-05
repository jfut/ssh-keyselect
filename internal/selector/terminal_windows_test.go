//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"

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
			reader, ok := terminal.reader.(*windowsTerminalInput)
			if !ok || reader.input != input || terminal.writer != output {
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

func TestCancelledInheritedPickerLeavesLaterInputForNextReader(t *testing.T) {
	t.Setenv("MSYSTEM", "MINGW64")
	t.Setenv("CYGWIN", "")
	for _, test := range []struct {
		live     bool
		blocking bool
		escape   bool
	}{
		{false, false, false}, {true, false, false}, {false, true, false}, {true, true, false},
		{false, false, true}, {true, false, true}, {false, true, true}, {true, true, true},
	} {
		t.Run(fmt.Sprintf("live=%t/blocking=%t/escape=%t", test.live, test.blocking, test.escape), func(t *testing.T) {
			input, writer, err := windowsPickerTestPipe(test.blocking)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = input.Close() }()
			defer func() { _ = writer.Close() }()
			oldStdin := os.Stdin
			os.Stdin = input
			defer func() { os.Stdin = oldStdin }()
			terminal, err := openTerminal("")
			if err != nil {
				t.Fatal(err)
			}
			terminal.writer = io.Discard
			defer func() { _ = terminal.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				options := makeSearchableIdentityOptions([]identity.Identity{{Comment: "key"}})
				var err error
				if test.live {
					_, err = (&TUISelector{}).selectLive(ctx, terminal, options, SelectionContext{}, time.Now())
				} else {
					_, err = (&TUISelector{}).selectLineBuffered(ctx, terminal, options, SelectionContext{}, time.Now())
				}
				done <- err
			}()
			// Give the pipe-backed picker a chance to block before cancellation.
			time.Sleep(20 * time.Millisecond)
			wantErr := error(context.Canceled)
			if test.escape {
				wantErr = ErrCancelled
				if _, err := writer.Write([]byte{0x1b}); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, wantErr) {
					t.Fatalf("selection error = %v, want %v", err, wantErr)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled picker did not stop its input reader")
			}
			if _, err := writer.Write([]byte("next\n")); err != nil {
				t.Fatal(err)
			}
			var later [5]byte
			if _, err := io.ReadFull(input, later[:]); err != nil || string(later[:]) != "next\n" {
				t.Fatalf("input after cancellation = %q, %v", later, err)
			}
		})
	}
}

// Native CreatePipe models the blocking handles inherited from Git Bash/Cygwin;
// os.Pipe also checks Go's overlapped pipe implementation.
func windowsPickerTestPipe(blocking bool) (*os.File, *os.File, error) {
	if !blocking {
		return os.Pipe()
	}
	var input, output windows.Handle
	if err := windows.CreatePipe(&input, &output, nil, 0); err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(input), "picker input"), os.NewFile(uintptr(output), "picker output"), nil
}
