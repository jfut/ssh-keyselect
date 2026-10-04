//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

func TestDefaultTUIEndpointUsesGitBashSocketForGitBashClient(t *testing.T) {
	t.Setenv("MSYSTEM", "MINGW64")
	t.Setenv("CYGWIN", "")
	endpoint, mode, err := DefaultTUIEndpointForMode("/c/Users/test/.ssh/agent/s.ssh-agent", transport.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if mode != transport.Cygwin {
		t.Fatalf("listen mode = %q, want %q", mode, transport.Cygwin)
	}
	if !strings.Contains(endpoint, "/.ssh/agent/s.ssh-keyselect.") || !strings.HasPrefix(endpoint, "/") {
		t.Fatalf("endpoint = %q, want a Git Bash socket in the user's SSH agent directory", endpoint)
	}
}

func TestDefaultTUIEndpointUsesNamedPipeForNativeClient(t *testing.T) {
	t.Setenv("MSYSTEM", "")
	t.Setenv("CYGWIN", "")
	endpoint, mode, err := DefaultTUIEndpointForMode(`\\.\pipe\openssh-ssh-agent`, transport.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if mode != transport.NamedPipe {
		t.Fatalf("listen mode = %q, want %q", mode, transport.NamedPipe)
	}
	if !strings.HasPrefix(endpoint, randomPipePrefix) {
		t.Fatalf("endpoint = %q, want a named pipe", endpoint)
	}
}
