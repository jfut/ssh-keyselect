//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

func newIntegrationEndpoint(t *testing.T, name string) *integrationEndpoint {
	t.Helper()
	// Use a short directory because Unix socket paths have platform-specific limits.
	dir, err := os.MkdirTemp("", "sk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, name+".sock")
	ln, cleanup, err := listener.ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEndpoint{
		path: path, mode: transport.Unix, listener: ln, cleanup: cleanup,
		dial: func() (net.Conn, error) { return net.Dial("unix", path) },
	}
}
