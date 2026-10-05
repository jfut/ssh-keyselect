// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

func TestIdentityAnswerUsesAgentWireFormat(t *testing.T) {
	want := identityAnswerFixture(t)
	message, err := MarshalIdentities([]identity.Identity{testIdentity("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(message, want) {
		t.Fatalf("identity answer = %x, want %x", message, want)
	}
	got, err := ParseIdentities(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Blob, testIdentity("alpha").Blob) || got[0].Comment != "alpha" {
		t.Fatalf("parsed identities = %+v, want alpha", got)
	}
}

func TestParseIdentitiesRejectsTrailingAndTruncatedData(t *testing.T) {
	valid := identityAnswerFixture(t)
	for _, message := range [][]byte{append(append([]byte(nil), valid...), 0), valid[:len(valid)-1]} {
		if _, err := ParseIdentities(message); !errors.Is(err, errMalformed) {
			t.Errorf("ParseIdentities(%x) error = %v, want malformed", message, err)
		}
	}
}

func TestIdentityAnswerHonorsFrameSizeLimit(t *testing.T) {
	id := testIdentity("")
	id.Comment = strings.Repeat("a", maxMessageSize-5-8-len(id.Blob))
	message, err := MarshalIdentities([]identity.Identity{id})
	if err != nil || len(message) != maxMessageSize {
		t.Fatalf("maximum-size identity answer = %d bytes, %v", len(message), err)
	}
	identities, err := ParseIdentities(message)
	if err != nil || len(identities) != 1 || identities[0].Comment != id.Comment {
		t.Fatalf("maximum-size identity answer could not be parsed: %v", err)
	}
	id.Comment += "a"
	if _, err := MarshalIdentities([]identity.Identity{id}); !errors.Is(err, errMalformed) {
		t.Fatalf("oversized identity answer = %v, want malformed", err)
	}
}

func TestSignRequestUsesAgentWireFormat(t *testing.T) {
	key := testIdentity("key").Blob
	wantData := []byte("session data")
	wantFlags := uint32(7)
	want, err := hex.DecodeString("0d000000330000000b7373682d656432353531390000002000000000000000000000000000000000000000000000000000000000000000000000000c73657373696f6e206461746100000007")
	if err != nil {
		t.Fatal(err)
	}
	gotKey, gotData, gotFlags, err := ParseSignRequest(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotKey, key) || !bytes.Equal(gotData, wantData) || gotFlags != wantFlags {
		t.Fatalf("parsed sign request = %x, %q, %d", gotKey, gotData, gotFlags)
	}
	if _, _, _, err := ParseSignRequest(append(want, 0)); !errors.Is(err, errMalformed) {
		t.Fatalf("trailing data error = %v, want malformed", err)
	}
}

func TestParseSessionBindExtension(t *testing.T) {
	hostKey := []byte("host key")
	sessionID := []byte("session id")
	signature := []byte("signature")
	payload := appendTestSSHString(nil, hostKey)
	payload = appendTestSSHString(payload, sessionID)
	payload = appendTestSSHString(payload, signature)
	payload = append(payload, 1)
	message := []byte{ExtensionRequest}
	message = appendTestSSHString(message, []byte(SessionBindExtension))
	message = append(message, payload...)

	extension, err := ParseExtensionRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	if extension.Name != SessionBindExtension {
		t.Fatalf("extension name = %q, want %q", extension.Name, SessionBindExtension)
	}
	request, err := ParseSessionBindRequest(extension.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(request.HostKey, hostKey) || !bytes.Equal(request.SessionID, sessionID) || !bytes.Equal(request.Signature, signature) || !request.IsForwarding {
		t.Fatalf("parsed session binding = %+v", request)
	}
	for _, malformed := range [][]byte{
		{RequestIdentities},
		{ExtensionRequest, 0, 0, 0, 0},
	} {
		if _, err := ParseExtensionRequest(malformed); err == nil {
			t.Errorf("ParseExtensionRequest(%x) accepted malformed input", malformed)
		}
	}
	invalidForwarding := append([]byte(nil), payload...)
	invalidForwarding[len(invalidForwarding)-1] = 2
	if _, err := ParseSessionBindRequest(invalidForwarding); !errors.Is(err, errMalformed) {
		t.Fatalf("invalid forwarding flag error = %v, want malformed", err)
	}
	for _, malformed := range [][]byte{nil, payload[:len(payload)-1], append(append([]byte(nil), payload...), 0)} {
		if _, err := ParseSessionBindRequest(malformed); !errors.Is(err, errMalformed) {
			t.Errorf("ParseSessionBindRequest(%x) error = %v, want malformed", malformed, err)
		}
	}
}

func TestReadFrameRejectsOversizedLength(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxMessageSize+1)
	if _, err := ReadFrame(bytes.NewReader(header[:])); err == nil {
		t.Fatal("ReadFrame accepted an oversized message")
	}
}

func testIdentity(comment string) identity.Identity {
	blob, err := hex.DecodeString("0000000b7373682d65643235353139000000200000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		panic(err)
	}
	return identity.Identity{Blob: blob, Comment: comment}
}

// identityAnswerFixture keeps parser input independent from MarshalIdentities.
func identityAnswerFixture(t *testing.T) []byte {
	t.Helper()
	message, err := hex.DecodeString("0c00000001000000330000000b7373682d6564323535313900000020000000000000000000000000000000000000000000000000000000000000000000000005616c706861")
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func appendTestSSHString(dst, value []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}
