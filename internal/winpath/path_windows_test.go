//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package winpath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeSocketPathConvertsGitBashDriveMount(t *testing.T) {
	got, err := NativeSocketPath("/c/Users/test/.ssh/agent/socket")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(`C:\Users\test\.ssh\agent\socket`)
	if got != want {
		t.Fatalf("native path = %q, want %q", got, want)
	}
}

func TestToGitBashPathUsesDriveMountFormat(t *testing.T) {
	nativePath := filepath.Join(t.TempDir(), "agent.sock")
	gitBashPath, err := ToGitBashPath(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	drive := strings.ToLower(filepath.VolumeName(nativePath)[:1])
	if !strings.HasPrefix(gitBashPath, "/"+drive+"/") || strings.Contains(gitBashPath, `\`) {
		t.Fatalf("Git Bash path = %q, want /%s/... with forward slashes", gitBashPath, drive)
	}
}
