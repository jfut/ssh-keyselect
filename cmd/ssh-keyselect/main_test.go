// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/branding"
)

func TestExecuteVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "ssh-keyselect dev (none)\n" {
		t.Fatalf("version output = %q", got)
	}
}

func TestExecuteHelpListsCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{
		"Commands:", "ssh", "list", "test", "version",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help does not contain %q", want)
		}
	}
	if description := strings.Join(strings.Fields(stdout.String()), " "); !strings.Contains(description, strings.Join(strings.Fields(branding.Description), " ")) {
		t.Errorf("help does not contain the application description: %s", stdout.String())
	}
	metadata := "Project URL: " + branding.ProjectURL + "\nAuthor: " + branding.Author
	if !strings.Contains(stdout.String(), metadata) {
		t.Errorf("help does not show project URL and author on separate lines: %s", stdout.String())
	}
}

func TestSSHHelpListsLogFileOption(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"ssh", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
	}
	help := strings.Join(strings.Fields(stdout.String()), " ")
	if !strings.Contains(help, "--log-file") {
		t.Fatalf("ssh help does not list --log-file: %s", stdout.String())
	}
}

func TestOpenLogFileReplacesPreviousContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh-keyselect.log")
	if err := os.WriteFile(path, []byte("old run"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("new run"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "new run"; got != want {
		t.Fatalf("log file contents = %q, want %q", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0600); got != want {
			t.Fatalf("log file permissions = %04o, want %04o", got, want)
		}
	}
}

func TestNewSSHCommandPassesArgumentsAndUsesTemporarySocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/upstream-agent.sock")
	args := []string{"-p", "2222", "user@example.org", "true"}
	command := newSSHCommand(context.Background(), args, "/tmp/ssh-keyselect.sock", io.Discard, io.Discard)

	if got, want := command.Args, append([]string{"ssh"}, args...); !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH command arguments = %q, want %q", got, want)
	}
	var socketValues []string
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "SSH_AUTH_SOCK=") {
			socketValues = append(socketValues, strings.TrimPrefix(entry, "SSH_AUTH_SOCK="))
		}
	}
	if !reflect.DeepEqual(socketValues, []string{"/tmp/ssh-keyselect.sock"}) {
		t.Fatalf("SSH_AUTH_SOCK values = %q, want the temporary proxy socket", socketValues)
	}
}

func TestListReportsAnUnavailableUpstream(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configPath := filepath.Join(configHome, "ssh-keyselect", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("invalid TOML = ["), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := execute([]string{"list"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "connect to upstream agent") {
		t.Fatalf("list result = code %d, stderr %q", code, stderr.String())
	}
}
