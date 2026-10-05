//go:build gui && windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

func TestGUIRejectsUpstreamEndpointAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	shellPath, err := winpath.ToGitBashPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		listen   string
		upstream string
		mode     transport.Mode
	}{
		{name: "native and Git Bash", listen: path, upstream: shellPath, mode: transport.Unix},
		{name: "Git Bash and native", listen: path, upstream: path, mode: transport.Cygwin},
		{name: "WSL1 and native", listen: "/mnt" + shellPath, upstream: path, mode: transport.WSL1},
		{name: "Cygwin and native", listen: "/cygdrive" + shellPath, upstream: path, mode: transport.Cygwin},
		{name: "pipe case", listen: `\\.\pipe\ssh-keyselect-test`, upstream: `\\.\PIPE\SSH-KEYSELECT-TEST`, mode: transport.NamedPipe},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Agent.Listen, cfg.Agent.Upstream = test.listen, test.upstream
			cfg.Agent.ListenMode = test.mode
			if _, _, err := resolveGUIListen(cfg); err == nil || !strings.Contains(err.Error(), "endpoints must be different") {
				t.Fatalf("GUI accepted equivalent endpoints %q and %q: %v", test.listen, test.upstream, err)
			}
		})
	}
}
