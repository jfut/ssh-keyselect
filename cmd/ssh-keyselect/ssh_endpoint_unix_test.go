//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSSHRejectsStaleUpstreamSocketWithoutReplacingIt(t *testing.T) {
	// Keep the path short enough for Unix sockets and preserve a real stale socket.
	directory, err := os.MkdirTemp("", "sk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	endpoint := filepath.Join(directory, "agent.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.Lstat(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"ssh", "--listen", endpoint, "--upstream", endpoint}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "listen and upstream endpoints must be different") {
		t.Fatalf("ssh result = code %d, stderr %q", code, stderr.String())
	}
	current, err := os.Lstat(endpoint)
	if err != nil || !os.SameFile(original, current) {
		t.Fatalf("stale upstream socket was removed or replaced: %v", err)
	}
}

func TestSSHRejectsUpstreamAtDefaultListenEndpoint(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", directory)
	t.Setenv("PATH", "")
	endpoint := filepath.Join(directory, "ssh-keyselect", "agent."+strconv.Itoa(os.Getpid()))
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"ssh", "--upstream", endpoint}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "listen and upstream endpoints must be different") {
		t.Fatalf("ssh result = code %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Lstat(endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default listen socket was created before rejecting the endpoints: %v", err)
	}
}
