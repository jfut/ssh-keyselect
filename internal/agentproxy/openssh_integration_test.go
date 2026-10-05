//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

// TestOpenSSHMaxAuthTries exercises the user-visible protection with a real ssh-agent and sshd.
func TestOpenSSHMaxAuthTries(t *testing.T) {
	const maxAuthTries = 2
	const identityCount = maxAuthTries + 1

	if runtime.GOOS != "linux" {
		t.Skip("the local sshd fixture is Linux-specific")
	}
	sshPath := requireTool(t, "ssh")
	sshAddPath := requireTool(t, "ssh-add")
	sshAgentPath := requireTool(t, "ssh-agent")
	sshKeygenPath := requireTool(t, "ssh-keygen")
	sshdPath, err := exec.LookPath("sshd")
	if err != nil {
		t.Skip("openssh-server is not installed")
	}
	sshdPrefix := sshdPrivilegePrefix(t)

	workDir := t.TempDir()
	upstreamSocket := filepath.Join(workDir, "upstream.sock")
	agentLog := &bytes.Buffer{}
	agentCmd := exec.Command(sshAgentPath, "-D", "-a", upstreamSocket)
	agentCmd.Stderr = agentLog
	if err := agentCmd.Start(); err != nil {
		t.Fatalf("start ssh-agent: %v", err)
	}
	t.Cleanup(func() { stopTestProcess(agentCmd) })
	waitForPath(t, upstreamSocket, agentCmd)

	keyPaths := make([]string, 0, identityCount)
	for i := 0; i < identityCount; i++ {
		keyPath := filepath.Join(workDir, fmt.Sprintf("key-%02d", i))
		output, err := exec.Command(sshKeygenPath, "-q", "-t", "ed25519", "-N", "", "-C", fmt.Sprintf("key-%02d", i), "-f", keyPath).CombinedOutput()
		if err != nil {
			t.Fatalf("generate test key %d: %v: %s", i, err, output)
		}
		keyPaths = append(keyPaths, keyPath)
	}
	addKeys := exec.Command(sshAddPath, keyPaths...)
	addKeys.Env = cmdutil.WithEnvironment(os.Environ(), "SSH_AUTH_SOCK", upstreamSocket)
	if output, err := addKeys.CombinedOutput(); err != nil {
		t.Fatalf("load test keys into ssh-agent: %v: %s", err, output)
	}
	listKeys := exec.Command(sshAddPath, "-L")
	listKeys.Env = cmdutil.WithEnvironment(os.Environ(), "SSH_AUTH_SOCK", upstreamSocket)
	listedKeys, err := listKeys.Output()
	if err != nil {
		t.Fatalf("list ssh-agent keys: %v", err)
	}
	keyLines := strings.Split(strings.TrimSpace(string(listedKeys)), "\n")
	if len(keyLines) != len(keyPaths) {
		t.Fatalf("ssh-agent identities = %d, want %d", len(keyLines), len(keyPaths))
	}
	correctKeyLine := keyLines[len(keyLines)-1]
	fields := strings.Fields(correctKeyLine)
	if len(fields) < 3 {
		t.Fatalf("last ssh-agent public key has no comment: %q", correctKeyLine)
	}
	correctComment := strings.Join(fields[2:], " ")
	authorizedKeysPath := filepath.Join(workDir, "authorized_keys")
	if err := os.WriteFile(authorizedKeysPath, []byte(correctKeyLine+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	hostKeyPath := filepath.Join(workDir, "host-key")
	if output, err := exec.Command(sshKeygenPath, "-q", "-t", "ed25519", "-N", "", "-f", hostKeyPath).CombinedOutput(); err != nil {
		t.Fatalf("generate sshd host key: %v: %s", err, output)
	}
	port := unusedTCPPort(t)
	sshdConfig := strings.Join([]string{
		"AddressFamily inet",
		"ListenAddress 127.0.0.1",
		fmt.Sprintf("Port %d", port),
		"HostKey " + hostKeyPath,
		"PidFile " + filepath.Join(workDir, "sshd.pid"),
		"AuthorizedKeysFile " + authorizedKeysPath,
		"StrictModes no",
		"PermitRootLogin yes",
		"AllowUsers root",
		"PubkeyAuthentication yes",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"UsePAM no",
		fmt.Sprintf("MaxAuthTries %d", maxAuthTries),
		"LogLevel VERBOSE",
	}, "\n") + "\n"
	sshdConfigPath := filepath.Join(workDir, "sshd_config")
	if err := os.WriteFile(sshdConfigPath, []byte(sshdConfig), 0600); err != nil {
		t.Fatal(err)
	}
	sshdArgs := append(append([]string(nil), sshdPrefix...), sshdPath, "-t", "-f", sshdConfigPath)
	if output, err := exec.Command(sshdArgs[0], sshdArgs[1:]...).CombinedOutput(); err != nil {
		t.Skipf("local sshd cannot run with this user/config: %v: %s", err, output)
	}
	sshdArgs = append(append([]string(nil), sshdPrefix...), sshdPath, "-D", "-e", "-f", sshdConfigPath)
	sshdCmd := exec.Command(sshdArgs[0], sshdArgs[1:]...)
	sshdCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	sshdLog := &bytes.Buffer{}
	sshdCmd.Stderr = sshdLog
	if err := sshdCmd.Start(); err != nil {
		t.Skipf("cannot start local sshd: %v", err)
	}
	t.Cleanup(func() { stopTestProcessGroup(sshdCmd) })
	if !waitForTCP(port, sshdCmd, 5*time.Second) {
		stopTestProcessGroup(sshdCmd)
		t.Skipf("local sshd did not start: %s", sshdLog.String())
	}

	frontendSocket := filepath.Join(workDir, "frontend.sock")
	frontend, cleanupSocket, err := listener.ListenWithMode(frontendSocket, transport.Unix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanupSocket)
	proxyCtx, cancelProxy := context.WithCancel(context.Background())
	proxyLog := &bytes.Buffer{}
	proxy := &Server{
		Agent:    upstream.EndpointAgent{Path: upstreamSocket},
		Selector: commentSelector{comment: correctComment},
		Logger:   slog.New(slog.NewTextHandler(proxyLog, nil)),
	}
	proxyDone := make(chan error, 1)
	go func() { proxyDone <- proxy.Serve(proxyCtx, frontend) }()
	t.Cleanup(func() {
		cancelProxy()
		select {
		case err := <-proxyDone:
			if err != nil {
				t.Errorf("stop proxy: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("proxy did not stop")
		}
	})

	sshArgsForTest := func() []string {
		return []string{
			"-F", "/dev/null", "-p", fmt.Sprint(port),
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=4",
			"-o", "IdentitiesOnly=no",
			"-o", "IdentityFile=none",
			"-o", "PreferredAuthentications=publickey",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "GlobalKnownHostsFile=/dev/null",
			"-o", "LogLevel=DEBUG3",
			"root@127.0.0.1", "echo success",
		}
	}
	direct := exec.Command(sshPath, sshArgsForTest()...)
	direct.Env = cmdutil.WithEnvironment(os.Environ(), "SSH_AUTH_SOCK", upstreamSocket)
	directOutput, directErr := direct.CombinedOutput()
	if directErr == nil {
		t.Fatalf("SSH without the proxy unexpectedly authenticated with a later key: %s", directOutput)
	}
	if !strings.Contains(string(directOutput), "Too many authentication failures") {
		t.Fatalf("SSH without the proxy failed for an unexpected reason (%v): %s", directErr, directOutput)
	}

	throughProxy := exec.Command(sshPath, sshArgsForTest()...)
	throughProxy.Env = cmdutil.WithEnvironment(os.Environ(), "SSH_AUTH_SOCK", frontendSocket)
	proxyOutput, proxyErr := throughProxy.CombinedOutput()
	if proxyErr != nil {
		t.Fatalf("SSH through the proxy failed: %v: %s\nproxy log:\n%s\nsshd log:\n%s", proxyErr, proxyOutput, proxyLog.String(), sshdLog.String())
	}
	if !strings.Contains(string(proxyOutput), "success") {
		t.Fatalf("SSH through the proxy output = %q, want success", proxyOutput)
	}
}

type commentSelector struct{ comment string }

func (s commentSelector) Select(_ context.Context, identities []identity.Identity, _ selector.SelectionContext) ([]identity.Identity, error) {
	for _, id := range identities {
		if id.Comment == s.comment {
			return []identity.Identity{id}, nil
		}
	}
	return nil, fmt.Errorf("test identity %q is missing", s.comment)
}

func requireTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return path
}

func sshdPrivilegePrefix(t *testing.T) []string {
	t.Helper()
	if os.Geteuid() == 0 {
		return nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		t.Skip("sshd needs root privileges and sudo is unavailable")
	}
	if output, err := exec.Command(sudo, "-n", "true").CombinedOutput(); err != nil {
		t.Skipf("sshd needs passwordless sudo: %v: %s", err, output)
	}
	return []string{sudo, "-n"}
}

func waitForPath(t *testing.T, path string, process *exec.Cmd) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if process.Process.Signal(syscall.Signal(0)) != nil {
			t.Fatalf("agent exited before creating its socket")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("agent did not create its socket")
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot allocate a loopback port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForTCP(port int, process *exec.Cmd, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		if process.Process.Signal(syscall.Signal(0)) != nil {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func stopTestProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

func stopTestProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	}
}

var _ selector.Selector = commentSelector{}
