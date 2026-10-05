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
	_, cleanup, err := ListenWithMode(path, transport.Unix)
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
}

func TestListenRemovesStaleSocket(t *testing.T) {
	path := filepath.Join(shortSocketTestDir(t), "agent.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatalf("Listen did not replace stale socket: %v", err)
	}
	defer cleanup()
}

func TestCleanupPreservesReplacementAtListenPath(t *testing.T) {
	path := filepath.Join(shortSocketTestDir(t), "agent.sock")
	_, cleanup, err := ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	const replacement = "another owner's file"
	if err := os.WriteFile(path, []byte(replacement), 0600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != replacement {
		t.Fatalf("cleanup removed or changed the replacement file: %q, %v", contents, err)
	}
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
