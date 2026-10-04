//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

func newIntegrationEndpoint(t *testing.T, name string) *integrationEndpoint {
	t.Helper()
	path := fmt.Sprintf(`\\.\pipe\ssh-keyselect-test-%d-%s`, os.Getpid(), name)
	ln, cleanup, err := listener.ListenWithMode(path, transport.NamedPipe)
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEndpoint{
		path: path, mode: transport.NamedPipe, listener: ln, cleanup: cleanup,
		dial: func() (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return winio.DialPipeContext(ctx, path)
		},
	}
}
