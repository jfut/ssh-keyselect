// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package identity parses and formats public identities carried by the SSH agent protocol.
package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/bits"
	"strconv"
	"strings"
	"unicode"

	"github.com/jfut/ssh-keyselect/internal/sshwire"
)

// Identity contains public information returned by an SSH agent. Blob is never a private key.
type Identity struct {
	Blob        []byte
	Comment     string
	Fingerprint string
	Algorithm   string
}

// New derives display metadata from an SSH public-key blob and its agent comment.
func New(blob, comment []byte) (Identity, error) {
	algorithm, rest, err := sshwire.ReadString(blob)
	if err != nil || len(algorithm) == 0 {
		return Identity{}, errors.New("invalid SSH public-key blob")
	}
	_ = rest // Algorithm-specific fields are opaque to the proxy.

	return Identity{
		Blob:        append([]byte(nil), blob...),
		Comment:     string(comment),
		Fingerprint: Fingerprint(blob),
		Algorithm:   string(algorithm),
	}, nil
}

// Fingerprint returns the OpenSSH-style display fingerprint for a public-key blob.
func Fingerprint(blob []byte) string {
	digest := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

// Digest returns the binary authorization key for this identity.
func Digest(blob []byte) [sha256.Size]byte { return sha256.Sum256(blob) }

// bitSize returns the effective public-key size in bits when the algorithm defines one.
func bitSize(blob []byte, algorithm string) (int, bool) {
	switch algorithm {
	case "ssh-ed25519", "sk-ssh-ed25519@openssh.com":
		return 255, true
	case "ssh-rsa":
		_, rest, err := sshwire.ReadString(blob)
		if err != nil {
			return 0, false
		}
		_, rest, err = sshwire.ReadString(rest) // Skip the public exponent.
		if err != nil {
			return 0, false
		}
		modulus, _, err := sshwire.ReadString(rest)
		if err != nil {
			return 0, false
		}
		for len(modulus) > 0 && modulus[0] == 0 {
			modulus = modulus[1:]
		}
		if len(modulus) == 0 {
			return 0, false
		}
		return (len(modulus)-1)*8 + bits.Len8(modulus[0]), true
	case "ecdsa-sha2-nistp256", "sk-ecdsa-sha2-nistp256@openssh.com":
		return 256, true
	case "ecdsa-sha2-nistp384":
		return 384, true
	case "ecdsa-sha2-nistp521":
		return 521, true
	default:
		return 0, false
	}
}

// DisplayBitSize formats a known key size for selector tables, or an em dash when unavailable.
func DisplayBitSize(blob []byte, algorithm string) string {
	if size, ok := bitSize(blob, algorithm); ok {
		return strconv.Itoa(size)
	}
	return "—"
}

// DisplayComment replaces terminal and bidirectional controls with spaces before rendering public text.
func DisplayComment(comment string) string {
	comment = strings.ToValidUTF8(comment, "�")
	return strings.Map(func(r rune) rune {
		// Bidi controls can disguise comments or reorder surrounding UI text without visible glyphs.
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return ' '
		}
		return r
	}, comment)
}
