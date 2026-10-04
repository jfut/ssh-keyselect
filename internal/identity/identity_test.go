// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package identity

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNewCreatesOpenSSHStyleFingerprint(t *testing.T) {
	blob, err := hex.DecodeString("0000000b7373682d656432353531390000000c7075626c6963206279746573")
	if err != nil {
		t.Fatal(err)
	}
	id, err := New(blob, []byte("work laptop"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := id.Fingerprint, "SHA256:fsU3nSBX+audvZkc/XR+QzGRsh5fUs9Al52NEDRc2iE"; got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
	if id.Algorithm != "ssh-ed25519" || id.Comment != "work laptop" {
		t.Fatalf("identity metadata = %+v", id)
	}
}

func TestNewRejectsEmptyAlgorithm(t *testing.T) {
	if _, err := New([]byte{0, 0, 0, 0}, nil); err == nil {
		t.Fatal("New accepted an SSH public-key blob with no algorithm")
	}
}

func TestDisplayCommentReplacesControlsAndInvalidUTF8(t *testing.T) {
	if got, want := DisplayComment("key\x1b[31m\nname\xff"), "key [31m name�"; got != want {
		t.Fatalf("DisplayComment() = %q, want %q", got, want)
	}
}

func TestDisplayCommentReplacesBidirectionalControls(t *testing.T) {
	// Cover directional marks, embeddings, overrides, and isolates, including unpaired controls.
	for _, control := range []rune{
		'\u061c', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069',
	} {
		t.Run(fmt.Sprintf("U+%04X", control), func(t *testing.T) {
			if got, want := DisplayComment("work"+string(control)+"key"), "work key"; got != want {
				t.Fatalf("DisplayComment() = %q, want %q", got, want)
			}
		})
	}
}

func TestDisplayCommentPreservesUnicodeText(t *testing.T) {
	// Ordinary RTL text, combining marks, and joiners must keep their spelling and glyph shaping.
	comment := "日本語 العربية עברית e\u0301 👩\u200d💻 می\u200cروم"
	if got := DisplayComment(comment); got != comment {
		t.Fatalf("DisplayComment() = %q, want %q", got, comment)
	}
}
