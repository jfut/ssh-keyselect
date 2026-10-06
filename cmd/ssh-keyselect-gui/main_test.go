//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/config"
)

func TestGUIListenCanBeResolvedWithoutAnUpstreamAgent(t *testing.T) {
	cfg := config.Default()
	path, mode, err := resolveGUIListen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || mode == "" {
		t.Fatalf("resolved listener = path %q, mode %q; both should be set", path, mode)
	}
}

func TestGUIDiagnosticsRetainOnlyABoundedTail(t *testing.T) {
	var output bytes.Buffer
	diagnostics := guiDiagnostics{output: &output}
	logger := cmdutil.NewLogger(&diagnostics, "warn")
	for i := range 10000 {
		logger.Warn("request rejected", "request", i)
	}
	if got := output.String(); strings.Count(got, "request rejected") != 10000 || !strings.Contains(got, "request=9999") {
		t.Fatal("running logs were not delivered immediately to stderr")
	}
	if got := diagnostics.String(); len(got) > 64*1024 || !strings.Contains(got, "request=9999") {
		t.Fatalf("diagnostic buffer retained %d bytes or lost the latest failure", len(got))
	}
	oversized := strings.Repeat("x", 128*1024) + "latest error"
	if n, err := diagnostics.Write([]byte(oversized)); err != nil || n != len(oversized) {
		t.Fatalf("oversized diagnostic write = %d, %v", n, err)
	}
	if got := diagnostics.String(); len(got) > 64*1024 || !strings.HasSuffix(got, "latest error") {
		t.Fatalf("oversized diagnostic write retained %d bytes or lost the tail", len(got))
	}
}

func TestExecuteVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "ssh-keyselect-gui dev (none)\n" {
		t.Fatalf("version output = %q", got)
	}
}

func TestGUIMainWindowTitleIncludesListenEndpointAndDirtyState(t *testing.T) {
	const endpoint = "C:¥Users¥jun¥.ssh¥ssh-keyselect-agent.sock"
	if got, want := guiMainWindowTitle(endpoint, true), endpoint+" - SSH KeySelect *"; got != want {
		t.Fatalf("GUI window title = %q, want %q", got, want)
	}
}
