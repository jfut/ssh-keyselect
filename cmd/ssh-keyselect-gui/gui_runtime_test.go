//go:build gui && !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
	"github.com/richardwilkes/unison"
	"golang.org/x/crypto/ssh/agent"
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
	if err := probeGUIRuntimeSocket(firstListen); err != nil {
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
	if err := probeGUIRuntimeSocket(secondListen); err != nil {
		t.Fatalf("updated GUI proxy did not listen: %v", err)
	}
	if err := probeGUIRuntimeSocket(firstListen); err == nil {
		t.Fatal("old listen endpoint is still accepting connections")
	}
}

func TestGUISettingsApplyAndShutdownWhilePickerWaitsForUI(t *testing.T) {
	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	directory, err := os.MkdirTemp("", "sk-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	upstreamPath := filepath.Join(directory, "upstream.sock")
	upstreamListener, err := net.Listen("unix", upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upstreamListener.Close() }()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey, Comment: "test key"}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()

	cfg := config.Default()
	cfg.Log.Level = "warn"
	cfg.Agent.Upstream, cfg.Agent.UpstreamMode = upstreamPath, transport.Unix
	cfg.Agent.Listen, cfg.Agent.ListenMode = filepath.Join(directory, "first.sock"), transport.Unix
	endpointAgent := &guiEndpointAgent{}
	picker := selector.NewGUISelector()
	server := &agentproxy.Server{Agent: endpointAgent, Selector: picker}
	runtime := &guiRuntime{ctx: ctx, server: server, agent: endpointAgent, loggerOutput: io.Discard, currentLogLevel: "off"}
	if _, _, err := runtime.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	state := &guiConfigState{runtime: runtime, cfg: cfg, actualListen: cfg.Agent.Listen}
	quitFinished := make(chan struct{})
	screen, err := unison.StartHeadless(unison.HeadlessConfig{Width: 900, Height: 600, SyncTimeout: time.Second},
		unison.StartupFinishedCallback(func() {
			window, err := unison.NewWindow("Settings test")
			if err != nil {
				t.Error(err)
				return
			}
			window.Show()
		}),
		unison.QuittingCallback(func() {
			guiShutdownProxy(picker, cancel, runtime)
			close(quitFinished)
		}))
	if err != nil {
		picker.Stop()
		runtime.Close()
		t.Fatal(err)
	}
	defer func() {
		picker.Stop()
		cancel()
		runtime.Close()
		screen.Stop()
	}()

	openWaitingClients := func() []net.Conn {
		var clients []net.Conn
		for range 2 {
			conn, err := net.Dial("unix", state.actualListen)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			clients = append(clients, conn)
			if err := protocol.WriteFrame(conn, []byte{protocol.RequestIdentities}); err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(time.Second)
		pickerOpen := false
		for !pickerOpen && time.Now().Before(deadline) {
			screen.Do(func() {
				for _, window := range unison.Windows() {
					pickerOpen = pickerOpen || strings.HasPrefix(window.Title(), "Select an SSH key")
				}
			})
			if !pickerOpen {
				time.Sleep(time.Millisecond)
			}
		}
		if !pickerOpen {
			t.Fatal("real agent request did not open the GUI picker")
		}
		return clients
	}

	// Exercise both rebinding the same socket and moving to another socket, with
	// one active modal picker and another client waiting for its selection slot.
	for index, path := range []string{cfg.Agent.Listen, filepath.Join(directory, "second.sock")} {
		clients := openWaitingClients()
		cfg.Agent.Listen = path
		cfg.Log.Level = []string{"info", "error"}[index]
		completed := make(chan error, 1)
		screen.Post(func() { state.apply(cfg, true, func(err error) { completed <- err }) })
		select {
		case err := <-completed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("applying settings deadlocked the UI and server shutdown")
		}
		for _, conn := range clients {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := protocol.ReadFrame(conn); err == nil {
				t.Fatal("old client remained active after applying settings")
			}
		}
		screen.Do(func() {
			if state.actualListen != path || !state.dirty || state.applying {
				t.Errorf("configuration did not finish applying: %+v", state)
			}
			if len(unison.Windows()) != 1 {
				t.Error("cancelled picker remained open after applying settings")
			}
		})
		if err := probeGUIRuntimeSocket(path); err != nil {
			t.Fatalf("replacement server is not accepting connections: %v", err)
		}
	}

	clients := openWaitingClients()
	screen.Post(unison.AttemptQuit)
	select {
	case <-quitFinished:
	case <-time.After(2 * time.Second):
		t.Fatal("quitting deadlocked the UI and server shutdown")
	}
	// Desktop Unison.Start never returns: cleanup must already be complete when
	// the quit callback returns, while clients are still waiting for selection.
	if _, err := os.Lstat(state.actualListen); !os.IsNotExist(err) {
		t.Fatalf("quit callback left the proxy socket behind: %v", err)
	}
	for _, conn := range clients {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		// Closing the modal picker can send a rejection before shutdown; require
		// EOF after draining that response rather than treating it as an open client.
		if _, err := io.Copy(io.Discard, conn); err != nil {
			t.Fatalf("client was not closed after quitting: %v", err)
		}
	}
	restartedAgent := &guiEndpointAgent{}
	restartedPicker := selector.NewGUISelector()
	defer restartedPicker.Stop()
	restarted := &guiRuntime{
		ctx: context.Background(), agent: restartedAgent,
		server:       &agentproxy.Server{Agent: restartedAgent, Selector: restartedPicker},
		loggerOutput: io.Discard, currentLogLevel: "off",
	}
	defer restarted.Close()
	if path, _, err := restarted.Apply(cfg); err != nil || path != cfg.Agent.Listen {
		t.Fatalf("proxy could not restart after quitting: path %q, error %v", path, err)
	}
	if err := probeGUIRuntimeSocket(cfg.Agent.Listen); err != nil {
		t.Fatalf("restarted proxy is not accepting connections: %v", err)
	}
}

// Probe the actual test listener without keeping a production API just for tests.
func probeGUIRuntimeSocket(path string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
