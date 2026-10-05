// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package upstream connects to an existing SSH agent endpoint.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

// RequestTimeout bounds connection setup and non-interactive requests, excluding picker and signing approval waits.
const RequestTimeout = 10 * time.Second

// Agent is the subset of SSH agent operations used by the proxy.
type Agent interface {
	List(context.Context) ([]identity.Identity, error)
	RoundTrip(context.Context, []byte) ([]byte, error)
	OpenSession(context.Context) (AgentSession, error)
}

// AgentSession keeps agent requests and their session bindings on one connection.
type AgentSession interface {
	Bind(context.Context, []byte) error
	List(context.Context) ([]identity.Identity, error)
	RoundTrip(context.Context, []byte) ([]byte, error)
	Close() error
}

// EndpointAgent connects to a local agent endpoint.
type EndpointAgent struct {
	Path string
	Mode transport.Mode
}

type agentEndpointSession struct {
	conn     net.Conn
	closeOne sync.Once
	closeErr error
}

// OpenSession keeps one upstream connection for the whole bound agent session.
func (a EndpointAgent) OpenSession(ctx context.Context) (AgentSession, error) {
	if a.Path == "" {
		return nil, errors.New("upstream endpoint is empty")
	}
	conn, err := dialEndpoint(ctx, a.Path, a.Mode)
	if err != nil {
		return nil, fmt.Errorf("connect to upstream agent: %w", err)
	}
	return &agentEndpointSession{conn: conn}, nil
}

func (s *agentEndpointSession) Bind(ctx context.Context, binding []byte) error {
	response, err := s.RoundTrip(ctx, binding)
	if err != nil {
		return fmt.Errorf("forward SSH session binding: %w", err)
	}
	if len(response) != 1 || response[0] != protocol.Success {
		return errors.New("upstream agent rejected SSH session binding")
	}
	return nil
}

func (s *agentEndpointSession) List(ctx context.Context) ([]identity.Identity, error) {
	response, err := s.RoundTrip(ctx, []byte{protocol.RequestIdentities})
	if err != nil {
		return nil, err
	}
	return protocol.ParseIdentities(response)
}

func (s *agentEndpointSession) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	if err := protocol.WriteFrame(s.conn, request); err != nil {
		return nil, fmt.Errorf("write upstream agent request: %w", err)
	}
	response, err := protocol.ReadFrame(s.conn)
	if err != nil {
		return nil, fmt.Errorf("read upstream agent response: %w", err)
	}
	return response, nil
}

func (s *agentEndpointSession) Close() error {
	s.closeOne.Do(func() { s.closeErr = s.conn.Close() })
	return s.closeErr
}

// List requests the upstream public identities.
func (a EndpointAgent) List(ctx context.Context) ([]identity.Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	response, err := a.RoundTrip(ctx, []byte{protocol.RequestIdentities})
	if err != nil {
		return nil, err
	}
	return protocol.ParseIdentities(response)
}

// RoundTrip forwards a single raw SSH agent payload and returns the upstream payload.
func (a EndpointAgent) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	session, err := a.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	// Signing can require user confirmation or a hardware-key touch; the caller controls cancellation.
	return session.RoundTrip(ctx, request)
}
