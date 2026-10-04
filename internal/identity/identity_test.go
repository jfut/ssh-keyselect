// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package identity

import (
	"encoding/hex"
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
