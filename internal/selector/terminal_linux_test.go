// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"golang.org/x/sys/unix"
)

func TestTerminalPickerCancellationStopsReadingAndRestoresTerminal(t *testing.T) {
	for _, cancellation := range []string{"context", "escape"} {
		t.Run(cancellation, func(t *testing.T) {
			// A real pseudoterminal exercises raw-mode restoration and Go's read poller.
			fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			master := os.NewFile(uintptr(fd), "picker pseudoterminal")
			defer func() { _ = master.Close() }()
			if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			path := "/dev/pts/" + strconv.Itoa(number)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := (&TUISelector{TTYPath: path}).Select(ctx, []identity.Identity{{Comment: "alpha"}}, SelectionContext{})
				finished <- err
			}()
			_ = master.SetReadDeadline(time.Now().Add(time.Second))
			var frame strings.Builder
			var data [1024]byte
			for !strings.Contains(frame.String(), identitySelectionPrompt) {
				n, err := master.Read(data[:])
				if err != nil {
					t.Fatalf("picker did not display a prompt: %v", err)
				}
				frame.Write(data[:n])
			}
			if _, err := master.Write([]byte("a")); err != nil {
				t.Fatal(err)
			}
			for strings.Count(frame.String(), identitySelectionPrompt) < 2 {
				n, err := master.Read(data[:])
				if err != nil {
					t.Fatalf("picker did not consume its filter input: %v", err)
				}
				frame.Write(data[:n])
			}
			wantErr := error(context.Canceled)
			if cancellation == "escape" {
				wantErr = ErrCancelled
				if _, err := master.Write([]byte{0x1b}); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, wantErr) {
					t.Fatalf("picker result = %v, want cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled picker did not join the terminal reader")
			}
			if cancellation == "escape" {
				// Drain the final prompt bytes before opening another reader or echoing later input.
				_ = master.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				for {
					n, err := master.Read(data[:])
					frame.Write(data[:n])
					if err != nil {
						break
					}
				}
				if !strings.HasSuffix(frame.String(), "\r\n") {
					t.Fatal("Escape left the cursor on the filter line instead of completing it")
				}
			}
			later, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = later.Close() }()
			_ = later.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := master.Write([]byte("later\n")); err != nil {
				t.Fatal(err)
			}
			var input [6]byte
			if _, err := io.ReadFull(later, input[:]); err != nil || string(input[:]) != "later\n" {
				t.Fatalf("terminal input after cancellation = %q, %v", input, err)
			}
		})
	}
}
