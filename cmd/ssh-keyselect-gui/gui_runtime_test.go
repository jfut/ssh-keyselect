//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

func TestGUIRuntimeRunsWithoutUpstreamAndAppliesEndpointChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	endpointAgent := &guiEndpointAgent{}
	endpointAgent.Set(upstream.EndpointAgent{Mode: transport.Unix})
	guiSelector := selector.NewGUISelector()
	defer guiSelector.Stop()
	server := &agentproxy.Server{Agent: endpointAgent, Selector: guiSelector}

	// Keep socket paths short enough for macOS Unix socket path limits.
	testDir, err := os.MkdirTemp("", "sk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(testDir) })
	firstListen := filepath.Join(testDir, "first.sock")
	secondListen := filepath.Join(testDir, "second.sock")
	runtime := &guiRuntime{
		ctx: ctx, server: server, agent: endpointAgent, loggerOutput: io.Discard,
		listenPath: firstListen, listenMode: transport.Unix, currentLogLevel: "off",
	}
	defer runtime.Close()

	cfg := config.Default()
	cfg.Agent.Listen = firstListen
	cfg.Agent.ListenMode = transport.Unix
	if path, mode, err := runtime.Apply(cfg); err != nil {
		t.Fatal(err)
	} else if path != firstListen || mode != transport.Unix {
		t.Fatalf("initial listen endpoint = %q (%q), want %q (unix)", path, mode, firstListen)
	}
	if err := listener.ProbeWithMode(firstListen, transport.Unix); err != nil {
		t.Fatalf("GUI proxy did not listen without an upstream agent: %v", err)
	}

	cfg.Agent.Upstream = filepath.Join(testDir, "upstream.sock")
	cfg.Agent.UpstreamMode = transport.Unix
	cfg.Agent.Listen = secondListen
	if path, _, err := runtime.Apply(cfg); err != nil {
		t.Fatal(err)
	} else if path != secondListen {
		t.Fatalf("updated listen endpoint = %q, want %q", path, secondListen)
	}
	if endpoint, _ := endpointAgent.Snapshot(); endpoint.Path != cfg.Agent.Upstream {
		t.Fatalf("upstream endpoint = %q, want %q", endpoint.Path, cfg.Agent.Upstream)
	}
	if err := listener.ProbeWithMode(secondListen, transport.Unix); err != nil {
		t.Fatalf("updated GUI proxy did not listen: %v", err)
	}
	if err := listener.ProbeWithMode(firstListen, transport.Unix); err == nil {
		t.Fatal("old listen endpoint is still accepting connections")
	}
}
