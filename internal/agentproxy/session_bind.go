// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"golang.org/x/crypto/ssh"
)

const (
	maxSessionBindings     = 16
	maxSessionBindingBytes = 1024 * 1024
	maxKnownHostsScanBytes = 1024 * 1024
	maxKnownHostsLineSize  = 64 * 1024
)

// verifiedSessionBind carries only the parsed binding data used to authorize a session.
type verifiedSessionBind struct {
	sessionID []byte
	raw       []byte
	display   selector.HostBinding
	// hostKey is kept only until the optional known_hosts display hint is resolved.
	hostKey ssh.PublicKey
}

// verifySessionBind accepts only host keys that verify their session identifier signature.
func verifySessionBind(message []byte) (verifiedSessionBind, error) {
	// The connection handler transfers ownership of this frame; retain it for replay
	// and use its session-ID slice directly instead of copying up to 1 MiB again.
	extension, err := protocol.ParseExtensionRequest(message)
	if err != nil {
		return verifiedSessionBind{}, err
	}
	if extension.Name != protocol.SessionBindExtension {
		return verifiedSessionBind{}, errors.New("unsupported agent extension")
	}
	request, err := protocol.ParseSessionBindRequest(extension.Payload)
	if err != nil {
		return verifiedSessionBind{}, err
	}
	hostKey, err := ssh.ParsePublicKey(request.HostKey)
	if err != nil {
		return verifiedSessionBind{}, errors.New("invalid session-bind host key")
	}
	var signature ssh.Signature
	if err := ssh.Unmarshal(request.Signature, &signature); err != nil || len(signature.Rest) != 0 {
		return verifiedSessionBind{}, errors.New("invalid session-bind signature")
	}
	if err := hostKey.Verify(request.SessionID, &signature); err != nil {
		return verifiedSessionBind{}, errors.New("session-bind host key signature did not verify")
	}
	return verifiedSessionBind{
		sessionID: request.SessionID,
		raw:       message,
		hostKey:   hostKey,
		display: selector.HostBinding{
			Algorithm:    hostKey.Type(),
			Fingerprint:  ssh.FingerprintSHA256(hostKey),
			IsForwarding: request.IsForwarding,
		},
	}, nil
}

// sessionKnownHostNames returns exact default known_hosts entries for this host key as display hints.
func sessionKnownHostNames(hostKey ssh.PublicKey) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{filepath.Join(home, ".ssh", "known_hosts")}
	if runtime.GOOS == "windows" {
		if programData := os.Getenv("ProgramData"); programData != "" {
			paths = append(paths, filepath.Join(programData, "ssh", "ssh_known_hosts"))
		}
	} else {
		paths = append(paths, "/etc/ssh/ssh_known_hosts")
	}

	const maxNames = 8
	hostKeyBlob := hostKey.Marshal()
	seen := make(map[string]struct{})
	var names []string
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(io.LimitReader(file, maxKnownHostsScanBytes))
		scanner.Buffer(make([]byte, 4096), maxKnownHostsLineSize)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			marker, hosts, key, _, _, err := ssh.ParseKnownHosts(line)
			if err != nil || marker != "" || key == nil || !bytes.Equal(key.Marshal(), hostKeyBlob) {
				continue
			}
			for _, name := range hosts {
				if name == "" || strings.ContainsAny(name, "*?!|") {
					continue
				}
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
				if len(names) == maxNames {
					break
				}
			}
			if len(names) == maxNames {
				break
			}
		}
		_ = file.Close()
		if len(names) == maxNames {
			break
		}
	}
	return names
}
