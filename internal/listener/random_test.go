// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

func TestDefaultTUIEndpointUsesPlatformDefault(t *testing.T) {
	t.Setenv("MSYSTEM", "")
	t.Setenv("CYGWIN", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	privateDir := filepath.Join(runtimeDir, "ssh-keyselect")
	if err := os.Mkdir(privateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateDir, 0755); err != nil {
		t.Fatal(err)
	}
	endpoint, mode, err := DefaultTUIEndpointForMode("", transport.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if mode != transport.NamedPipe {
			t.Fatalf("listen mode = %q, want %q", mode, transport.NamedPipe)
		}
		if !strings.HasPrefix(endpoint, `\\.\pipe\ssh-keyselect.`) {
			t.Fatalf("endpoint = %q, want a named pipe", endpoint)
		}
		second, _, err := DefaultTUIEndpointForMode("", transport.Auto)
		if err != nil {
			t.Fatal(err)
		}
		if endpoint == second {
			t.Fatalf("two default Windows endpoints are identical: %q", endpoint)
		}
		return
	}
	if mode != transport.Unix {
		t.Fatalf("listen mode = %q, want %q", mode, transport.Unix)
	}
	want := filepath.Join(runtimeDir, "ssh-keyselect", "agent."+strconv.Itoa(os.Getpid()))
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
	info, err := os.Stat(filepath.Dir(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("runtime directory permissions = %04o, want 0700", got)
	}
}
