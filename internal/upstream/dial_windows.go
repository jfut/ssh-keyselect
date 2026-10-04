//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
	"github.com/jfut/ssh-keyselect/internal/winsocket"
)

func dialEndpoint(ctx context.Context, path string, requestedMode transport.Mode) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	mode, err := ResolveMode(path, requestedMode)
	if err != nil {
		return nil, err
	}
	switch mode {
	case transport.NamedPipe:
		if !winpath.IsNamedPipe(path) {
			return nil, errors.New("named-pipe upstream mode requires a named-pipe endpoint")
		}
		return winio.DialPipeContext(dialCtx, path)
	case transport.Cygwin:
		return winsocket.Dial(dialCtx, path, mode)
	case transport.Unix, transport.WSL1:
		return dialWindowsUnixSocket(dialCtx, path)
	default:
		return nil, fmt.Errorf("unsupported Windows upstream mode %q", mode)
	}

}

// ResolveMode reports the endpoint transport that an agent connection will use.
func ResolveMode(path string, requestedMode transport.Mode) (transport.Mode, error) {
	mode, err := transport.ParseMode(string(requestedMode))
	if err != nil {
		return "", err
	}
	if mode == transport.Auto {
		return winsocket.DetectMode(path)
	}
	return mode, nil
}

func dialWindowsUnixSocket(ctx context.Context, path string) (net.Conn, error) {
	nativePath, err := winpath.NativeSocketPath(path)
	if err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", nativePath)
}
