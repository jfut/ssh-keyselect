//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

func TestDefaultTUIEndpointSeparatesUsersInTemporaryDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", t.TempDir())
	// A legacy shared directory must not interfere with the current user's socket.
	shared := filepath.Join(os.TempDir(), "ssh-keyselect")
	if err := os.Mkdir(shared, 0755); err != nil {
		t.Fatal(err)
	}
	endpoint, _, err := DefaultTUIEndpointForMode("", transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.TempDir(), "ssh-keyselect-"+strconv.Itoa(os.Geteuid()))
	if filepath.Dir(endpoint) != want {
		t.Fatalf("socket directory = %q, want UID-specific %q", filepath.Dir(endpoint), want)
	}
	info, err := os.Stat(want)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private runtime directory = %v, %v", info, err)
	}
}

func TestDefaultTUIEndpointRejectsSymlinkDirectory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(base, "ssh-keyselect")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DefaultTUIEndpointForMode("", transport.Unix); err == nil {
		t.Fatal("accepted a symlink as the private runtime directory")
	}
}

func TestDefaultTUIEndpointRejectsForeignRuntimeOwner(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the root directory belongs to this process's UID")
	}
	t.Setenv("XDG_RUNTIME_DIR", "/")
	if _, _, err := DefaultTUIEndpointForMode("", transport.Unix); err == nil || !strings.Contains(err.Error(), "not owned by the current user") {
		t.Fatalf("foreign runtime directory error = %v, want ownership rejection", err)
	}
}

func TestDefaultTUIEndpointDoesNotChmodForeignPrivateDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing the fixture's owner requires root")
	}
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	directory := filepath.Join(base, "ssh-keyselect")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(directory, 65534, -1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DefaultTUIEndpointForMode("", transport.Unix); err == nil || !strings.Contains(err.Error(), "not owned by the current user") {
		t.Fatalf("foreign private directory error = %v, want ownership rejection", err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("foreign directory permissions changed: %v, %v", info, err)
	}
}
