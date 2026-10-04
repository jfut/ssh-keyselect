//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

func newIntegrationEndpoint(t *testing.T, name string) *integrationEndpoint {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".sock")
	ln, cleanup, err := listener.ListenWithMode(path, transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEndpoint{
		path: path, mode: transport.Unix, listener: ln, cleanup: cleanup,
		dial: func() (net.Conn, error) { return net.Dial("unix", path) },
	}
}
