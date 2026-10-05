//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package upstream

import (
	"context"
	"fmt"
	"net"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

func dialEndpoint(ctx context.Context, path string, requestedMode transport.Mode) (net.Conn, error) {
	_, err := ResolveMode(path, requestedMode)
	if err != nil {
		return nil, err
	}
	return (&net.Dialer{Timeout: RequestTimeout}).DialContext(ctx, "unix", path)
}

// ResolveMode reports the endpoint transport that an agent connection will use.
func ResolveMode(_ string, requestedMode transport.Mode) (transport.Mode, error) {
	mode, err := transport.ParseMode(string(requestedMode))
	if err != nil {
		return "", err
	}
	switch mode {
	case transport.Auto, transport.Unix, transport.WSL1:
		return transport.Unix, nil
	default:
		return "", fmt.Errorf("upstream mode %q is supported only on Windows", mode)
	}
}
