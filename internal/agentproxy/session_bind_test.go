// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/jfut/ssh-keyselect/internal/protocol"
)

func TestVerifySessionBindRequiresValidHostSignature(t *testing.T) {
	sessionID := []byte("test SSH session")
	message, hostKey := testSessionBindMessage(t, sessionID, true)

	binding, err := verifySessionBind(message)
	if err != nil {
		t.Fatal(err)
	}
	if binding.display.Algorithm != hostKey.Type() || binding.display.Fingerprint != ssh.FingerprintSHA256(hostKey) || !binding.display.IsForwarding {
		t.Fatalf("verified host details = %+v", binding.display)
	}

	message[len(message)-2] ^= 1
	if _, err := verifySessionBind(message); err == nil {
		t.Fatal("session binding with a modified host signature was accepted")
	}
}

func testSessionBindMessage(t *testing.T, sessionID []byte, forwarding bool) ([]byte, ssh.PublicKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(rand.Reader, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	payload := appendSessionSSHString(nil, signer.PublicKey().Marshal())
	payload = appendSessionSSHString(payload, sessionID)
	payload = appendSessionSSHString(payload, ssh.Marshal(signature))
	if forwarding {
		payload = append(payload, 1)
	} else {
		payload = append(payload, 0)
	}
	message := []byte{protocol.ExtensionRequest}
	message = appendSessionSSHString(message, []byte(protocol.SessionBindExtension))
	message = append(message, payload...)
	return message, signer.PublicKey()
}

func appendSessionSSHString(dst, value []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}
