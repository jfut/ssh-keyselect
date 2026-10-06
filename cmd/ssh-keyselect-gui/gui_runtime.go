//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

// resolveGUIListen chooses a frontend endpoint without requiring an upstream agent.
func resolveGUIListen(cfg config.Config) (string, transport.Mode, error) {
	cfg.Agent.Upstream = config.ExpandPath(cfg.Agent.Upstream)
	path := config.ExpandPath(cfg.Agent.Listen)
	var mode transport.Mode
	var err error
	if path == "" {
		path, mode, err = guiDefaultListenEndpoint(cfg.Agent.Upstream, cfg.Agent.ListenMode)
	} else {
		mode, err = listener.ResolveMode(path, cfg.Agent.Upstream, cfg.Agent.ListenMode)
	}
	if err != nil {
		return "", "", err
	}
	path = guiNormalizeListenPathForMode(path, mode)
	if transport.SameEndpoint(path, cfg.Agent.Upstream) {
		return "", "", errors.New("listen and upstream endpoints must be different")
	}
	return path, mode, nil
}

// guiEndpointAgent lets the running proxy and refresh action safely use the latest endpoint.
type guiEndpointAgent struct {
	mu       sync.RWMutex
	endpoint upstream.EndpointAgent
	version  uint64
}

func (a *guiEndpointAgent) Set(endpoint upstream.EndpointAgent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.endpoint != endpoint {
		a.endpoint = endpoint
		a.version++
	}
}

func (a *guiEndpointAgent) Snapshot() (upstream.EndpointAgent, uint64) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.endpoint, a.version
}

func (a *guiEndpointAgent) List(ctx context.Context) ([]identity.Identity, error) {
	endpoint, _ := a.Snapshot()
	return endpoint.List(ctx)
}

func (a *guiEndpointAgent) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	endpoint, _ := a.Snapshot()
	return endpoint.RoundTrip(ctx, request)
}

func (a *guiEndpointAgent) OpenSession(ctx context.Context) (upstream.AgentSession, error) {
	endpoint, _ := a.Snapshot()
	return endpoint.OpenSession(ctx)
}

type guiListenerState struct {
	cleanup func()
	cancel  context.CancelFunc
	done    chan error
	active  atomic.Bool
}

// guiRuntime restarts serving at a changed endpoint and invalidates old client selections.
type guiRuntime struct {
	mu              sync.Mutex
	ctx             context.Context
	server          *agentproxy.Server
	agent           *guiEndpointAgent
	loggerOutput    io.Writer
	onServeError    func(error)
	listenerState   *guiListenerState
	listenPath      string
	listenMode      transport.Mode
	currentLogLevel string
	currentLogFile  string
	logOutput       *guiLogOutput
	logLevel        *slog.LevelVar
}

func (r *guiRuntime) Apply(cfg config.Config) (string, transport.Mode, error) {
	// Apply and Close run outside the UI thread; serialize their listener lifetimes.
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return "", "", err
	}
	if err := cfg.Validate(); err != nil {
		return "", "", err
	}
	cfg.Log.File = config.ExpandPath(cfg.Log.File)
	logFileChanged := r.currentLogFile != cfg.Log.File
	var preparedLogFile *os.File
	if logFileChanged {
		if r.logOutput == nil {
			return "", "", errors.New("GUI log output is unavailable")
		}
		var err error
		preparedLogFile, err = r.logOutput.open(cfg.Log.File)
		if err != nil {
			return "", "", err
		}
	}
	defer func() {
		if preparedLogFile != nil {
			_ = preparedLogFile.Close()
		}
	}()
	applyAgentSettings := func() {
		r.agent.Set(upstream.EndpointAgent{Path: cfg.Agent.Upstream, Mode: cfg.Agent.UpstreamMode})
		if logFileChanged {
			r.logOutput.replace(preparedLogFile)
			preparedLogFile = nil
			r.currentLogFile = cfg.Log.File
		}
		if r.currentLogLevel != cfg.Log.Level {
			if r.logLevel != nil {
				r.logLevel.Set(guiSlogLevel(cfg.Log.Level))
			} else {
				r.server.Logger = cmdutil.NewLogger(r.loggerOutput, cfg.Log.Level)
				slog.SetDefault(r.server.Logger)
			}
			r.currentLogLevel = cfg.Log.Level
		}
	}
	cfg.Agent.Upstream = config.ExpandPath(cfg.Agent.Upstream)
	path, mode, err := resolveGUIListen(cfg)
	if err != nil {
		return "", "", err
	}
	if transport.SameEndpoint(path, r.listenPath) {
		path = r.listenPath
	}

	hadListener := r.listenerState != nil
	if hadListener && r.listenerState.active.Load() && mode == r.listenMode && transport.SameEndpoint(path, r.listenPath) {
		applyAgentSettings()
		return r.listenPath, mode, nil
	}
	if !hadListener {
		exists, err := guiListenPathExists(path)
		if err != nil {
			return "", "", fmt.Errorf("inspect listen path: %w", err)
		}
		if exists {
			r.listenPath = ""
			r.listenMode = mode
			applyAgentSettings()
			if r.server.Logger != nil {
				r.server.Logger.Warn("listen path already exists; leaving the proxy unconfigured", "listen", path)
			}
			return "", mode, nil
		}
	}

	var ln net.Listener
	var cleanup func()
	// Rebinding the same filesystem location under a different Windows transport
	// requires closing our current listener first so its owned socket file is removed.
	if !hadListener || !transport.SameEndpoint(path, r.listenPath) {
		ln, cleanup, err = listener.ListenWithMode(path, mode)
		if err != nil {
			return "", "", err
		}
	}

	oldPath, oldMode := r.listenPath, r.listenMode
	r.stopListener()
	if ln == nil {
		ln, cleanup, err = listener.ListenWithMode(path, mode)
		if err != nil {
			if hadListener {
				if oldListener, oldCleanup, restoreErr := listener.ListenWithMode(oldPath, oldMode); restoreErr == nil {
					r.startListener(oldListener, oldCleanup)
				}
			}
			return "", "", err
		}
	}

	applyAgentSettings()
	r.listenPath, r.listenMode = path, mode
	r.startListener(ln, cleanup)
	if r.server.Logger != nil {
		r.server.Logger.Info("SSH agent proxy started", "listen", path, "ui", "gui")
	}
	return path, mode, nil
}

func (r *guiRuntime) startListener(ln net.Listener, cleanup func()) {
	serveCtx, cancel := context.WithCancel(r.ctx)
	state := &guiListenerState{cleanup: cleanup, cancel: cancel, done: make(chan error, 1)}
	state.active.Store(true)
	r.listenerState = state
	logger := r.server.Logger
	go func() {
		err := r.server.Serve(serveCtx, ln)
		state.active.Store(false)
		state.done <- err
		if err != nil && serveCtx.Err() == nil {
			logger.Error("SSH agent proxy stopped", "error", err)
			if r.onServeError != nil {
				r.onServeError(err)
			}
		}
	}()
}

func (r *guiRuntime) stopListener() {
	state := r.listenerState
	if state == nil {
		return
	}
	r.listenerState = nil
	state.cancel()
	<-state.done
	if state.cleanup != nil {
		state.cleanup()
	}
}

func (r *guiRuntime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopListener()
}

var _ upstream.Agent = (*guiEndpointAgent)(nil)
