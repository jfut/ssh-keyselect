//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

func TestListenProtectsAndRemovesSocket(t *testing.T) {
	path := filepath.Join(shortSocketTestDir(t), "nested", "agent.sock")
	ln, cleanup, err := ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket permissions = %04o, want 0600", got)
	}
	if _, _, err := ListenWithMode(path, transport.Unix); err == nil {
		t.Fatal("second listener succeeded on an active socket")
	}
	cleanup()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains after cleanup: %v", err)
	}
	_ = ln
}

func TestListenRemovesStaleSocket(t *testing.T) {
	path := filepath.Join(shortSocketTestDir(t), "agent.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	ln, cleanup, err := ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatalf("Listen did not replace stale socket: %v", err)
	}
	defer cleanup()
	_ = ln
}

// Short directory names keep Unix socket paths below platform limits.
func shortSocketTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
