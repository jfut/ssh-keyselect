// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package identity

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNewCreatesOpenSSHStyleFingerprint(t *testing.T) {
	blob, err := hex.DecodeString("0000000b7373682d65643235353139000000200000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	id, err := New(blob, []byte("work laptop"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := id.Fingerprint, "SHA256:kmYcvdi2GkPeWxB6XLjrZB8JHsy2Hm8luHMFp9GMvqk"; got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
	if id.Algorithm != "ssh-ed25519" || id.Comment != "work laptop" {
		t.Fatalf("identity metadata = %+v", id)
	}
}

func TestNewRejectsMalformedAndUnsafePublicKeys(t *testing.T) {
	valid, err := hex.DecodeString("0000000b7373682d65643235353139000000200000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	unsafeAlgorithm := []byte("ssh-ed25519\x1b]52;c;ZXZpbA==\a")
	injected := binary.BigEndian.AppendUint32(nil, uint32(len(unsafeAlgorithm)))
	injected = append(injected, unsafeAlgorithm...)
	injected = append(injected, valid[15:]...)
	for _, blob := range [][]byte{nil, valid[:15], valid[:len(valid)-1], append(bytes.Clone(valid), 0), injected} {
		if _, err := New(blob, nil); err == nil {
			t.Errorf("New accepted malformed public-key blob %x", blob)
		}
	}
}

func TestNewRejectsEmptyAlgorithm(t *testing.T) {
	if _, err := New([]byte{0, 0, 0, 0}, nil); err == nil {
		t.Fatal("New accepted an SSH public-key blob with no algorithm")
	}
}

func TestDisplayTextReplacesControlsAndInvalidUTF8(t *testing.T) {
	if got, want := DisplayText("key\x1b[31m\nname\xff"), "key [31m name�"; got != want {
		t.Fatalf("DisplayText() = %q, want %q", got, want)
	}
}

func TestDisplayTextReplacesBidirectionalControls(t *testing.T) {
	// Cover directional marks, embeddings, overrides, and isolates, including unpaired controls.
	for _, control := range []rune{
		'\u061c', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069',
	} {
		t.Run(fmt.Sprintf("U+%04X", control), func(t *testing.T) {
			if got, want := DisplayText("work"+string(control)+"key"), "work key"; got != want {
				t.Fatalf("DisplayText() = %q, want %q", got, want)
			}
		})
	}
}

func TestDisplayTextPreservesUnicodeText(t *testing.T) {
	// Ordinary RTL text, combining marks, and joiners must keep their spelling and glyph shaping.
	comment := "日本語 العربية עברית e\u0301 👩\u200d💻 می\u200cروم"
	if got := DisplayText(comment); got != comment {
		t.Fatalf("DisplayText() = %q, want %q", got, comment)
	}
}
