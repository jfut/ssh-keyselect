//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGUIExportCommandPreservesShellMetacharacters(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell is unavailable")
	}
	marker := filepath.Join(t.TempDir(), "injected")
	// Execute the copied command in a real shell; a broken quote creates a harmless marker.
	for _, path := range []string{
		"/tmp/$(touch '" + marker + "')/agent.sock",
		"/tmp/key ' \" \\ `printf injected` $HOME ;\nagent.sock",
		`C:\Users\key's directory\$USER\agent.sock`,
	} {
		command := guiEndpointExportCommand("SSH_AUTH_SOCK", path) + "\nprintf '%s' \"$SSH_AUTH_SOCK\""
		output, err := exec.Command(shell, "-c", command).Output()
		if err != nil || string(output) != path {
			t.Fatalf("copied command changed endpoint text: output %q, error %v", output, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("copied command executed path content: %v", err)
	}
}
