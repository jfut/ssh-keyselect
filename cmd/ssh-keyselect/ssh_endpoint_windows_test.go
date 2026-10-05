//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/winpath"
)

func TestSSHRejectsWindowsEndpointAliases(t *testing.T) {
	directory := t.TempDir()
	endpoint := filepath.Join(directory, "agent.sock")
	gitBashPath, err := winpath.ToGitBashPath(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	pipe := `\\.\pipe\ssh-keyselect-test.` + filepath.Base(directory)
	t.Setenv("PATH", "")
	tests := []struct {
		name     string
		listen   string
		upstream string
	}{
		{name: "pipe case", listen: pipe, upstream: strings.ToUpper(pipe)},
		{name: "path case", listen: endpoint, upstream: strings.ToUpper(endpoint)},
		{name: "Git Bash listen", listen: gitBashPath, upstream: endpoint},
		{name: "Git Bash upstream", listen: endpoint, upstream: gitBashPath},
		{name: "WSL1 listen", listen: "/mnt" + gitBashPath, upstream: endpoint},
		{name: "WSL1 upstream", listen: endpoint, upstream: "/mnt" + gitBashPath},
		{name: "Cygwin listen", listen: "/cygdrive" + gitBashPath, upstream: endpoint},
		{name: "Cygwin upstream", listen: endpoint, upstream: "/cygdrive" + gitBashPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := execute([]string{"ssh", "--listen", tt.listen, "--upstream", tt.upstream}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "listen and upstream endpoints must be different") {
				t.Fatalf("ssh result = code %d, stderr %q", code, stderr.String())
			}
		})
	}
}
