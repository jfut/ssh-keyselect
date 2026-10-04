// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package protocol implements the bounded SSH agent message framing used by the proxy.
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/sshwire"
)

const (
	RequestIdentities byte = 11
	IdentitiesAnswer  byte = 12
	SignRequest       byte = 13
	SignResponse      byte = 14
	ExtensionRequest  byte = 27
	Failure           byte = 5
	Success           byte = 6

	AddIdentity       byte = 17
	RemoveIdentity    byte = 18
	RemoveAllIdentity byte = 19
	AddSmartcard      byte = 20
	RemoveSmartcard   byte = 21
	Lock              byte = 22
	Unlock            byte = 23

	SessionBindExtension = "session-bind@openssh.com"

	// maxMessageSize bounds allocations made from client-controlled packet lengths.
	maxMessageSize = 1024 * 1024
)

var errMalformed = errors.New("malformed SSH agent message")

// ReadFrame reads one length-prefixed SSH agent message.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxMessageSize {
		return nil, fmt.Errorf("invalid SSH agent message length %d", n)
	}
	message := make([]byte, int(n))
	if _, err := io.ReadFull(r, message); err != nil {
		return nil, err
	}
	return message, nil
}

// WriteFrame writes one length-prefixed SSH agent message.
func WriteFrame(w io.Writer, message []byte) error {
	if len(message) == 0 || len(message) > maxMessageSize {
		return fmt.Errorf("invalid SSH agent message length %d", len(message))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(message)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, message)
}

// ParseIdentities validates and decodes an IDENTITIES_ANSWER payload.
func ParseIdentities(message []byte) ([]identity.Identity, error) {
	if len(message) < 5 || message[0] != IdentitiesAnswer {
		return nil, errMalformed
	}
	count := binary.BigEndian.Uint32(message[1:5])
	if uint64(count) > uint64((len(message)-5)/8) {
		return nil, errMalformed
	}
	identities := make([]identity.Identity, 0, int(count))
	reader := messageReader{data: message[5:]}
	for i := uint32(0); i < count; i++ {
		blob, err := reader.sshString()
		if err != nil {
			return nil, errMalformed
		}
		comment, err := reader.sshString()
		if err != nil {
			return nil, errMalformed
		}
		id, err := identity.New(blob, comment)
		if err != nil {
			return nil, errMalformed
		}
		identities = append(identities, id)
	}
	if reader.remaining() != 0 {
		return nil, errMalformed
	}
	return identities, nil
}

// MarshalIdentities creates an IDENTITIES_ANSWER payload.
func MarshalIdentities(identities []identity.Identity) ([]byte, error) {
	if uint64(len(identities)) > uint64(maxMessageSize/8) {
		return nil, errMalformed
	}
	message := make([]byte, 5, 5+len(identities)*64)
	message[0] = IdentitiesAnswer
	binary.BigEndian.PutUint32(message[1:5], uint32(len(identities)))
	for _, id := range identities {
		var err error
		message, err = appendSSHString(message, id.Blob)
		if err != nil {
			return nil, err
		}
		message, err = appendSSHString(message, []byte(id.Comment))
		if err != nil {
			return nil, err
		}
	}
	if len(message) > maxMessageSize {
		return nil, errMalformed
	}
	return message, nil
}

// ParseSignRequest validates a SIGN_REQUEST payload and returns its key, data, and flags.
func ParseSignRequest(message []byte) (keyBlob, data []byte, flags uint32, err error) {
	if len(message) < 1 || message[0] != SignRequest {
		return nil, nil, 0, errMalformed
	}
	reader := messageReader{data: message[1:]}
	keyBlob, err = reader.sshString()
	if err != nil {
		return nil, nil, 0, errMalformed
	}
	data, err = reader.sshString()
	if err != nil || reader.remaining() != 4 {
		return nil, nil, 0, errMalformed
	}
	flags = binary.BigEndian.Uint32(reader.data[:4])
	return keyBlob, data, flags, nil
}

// MarshalSignRequest creates a SIGN_REQUEST payload.
func MarshalSignRequest(keyBlob, data []byte, flags uint32) ([]byte, error) {
	message := []byte{SignRequest}
	var err error
	if message, err = appendSSHString(message, keyBlob); err != nil {
		return nil, err
	}
	if message, err = appendSSHString(message, data); err != nil {
		return nil, err
	}
	var encodedFlags [4]byte
	binary.BigEndian.PutUint32(encodedFlags[:], flags)
	message = append(message, encodedFlags[:]...)
	if len(message) > maxMessageSize {
		return nil, errMalformed
	}
	return message, nil
}

// AgentExtension contains the name and payload of an SSH agent extension request.
type AgentExtension struct {
	Name    string
	Payload []byte
}

// ParseExtensionRequest decodes the extension name and leaves its payload opaque.
func ParseExtensionRequest(message []byte) (AgentExtension, error) {
	if len(message) < 1 || message[0] != ExtensionRequest {
		return AgentExtension{}, errMalformed
	}
	reader := messageReader{data: message[1:]}
	name, err := reader.sshString()
	if err != nil || len(name) == 0 {
		return AgentExtension{}, errMalformed
	}
	return AgentExtension{Name: string(name), Payload: append([]byte(nil), reader.data...)}, nil
}

// SessionBindRequest contains the host identity bound to one SSH session.
type SessionBindRequest struct {
	HostKey      []byte
	SessionID    []byte
	Signature    []byte
	IsForwarding bool
}

// ParseSessionBindRequest decodes the payload of session-bind@openssh.com.
func ParseSessionBindRequest(payload []byte) (SessionBindRequest, error) {
	reader := messageReader{data: payload}
	hostKey, err := reader.sshString()
	if err != nil || len(hostKey) == 0 {
		return SessionBindRequest{}, errMalformed
	}
	sessionID, err := reader.sshString()
	if err != nil || len(sessionID) == 0 {
		return SessionBindRequest{}, errMalformed
	}
	signature, err := reader.sshString()
	if err != nil || len(signature) == 0 || len(reader.data) != 1 || reader.data[0] > 1 {
		return SessionBindRequest{}, errMalformed
	}
	return SessionBindRequest{
		HostKey:      hostKey,
		SessionID:    sessionID,
		Signature:    signature,
		IsForwarding: reader.data[0] == 1,
	}, nil
}

func appendSSHString(dst, value []byte) ([]byte, error) {
	if uint64(len(value)) > uint64(maxMessageSize) || len(dst)+4+len(value) > maxMessageSize {
		return nil, errMalformed
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...), nil
}

type messageReader struct {
	data []byte
}

func (r *messageReader) sshString() ([]byte, error) {
	value, rest, err := sshwire.ReadString(r.data)
	if err != nil {
		return nil, errMalformed
	}
	r.data = rest
	return value, nil
}

func (r *messageReader) remaining() int { return len(r.data) }

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
