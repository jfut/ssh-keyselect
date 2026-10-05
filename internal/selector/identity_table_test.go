// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
)

func TestFormatIdentityTableSanitizesComments(t *testing.T) {
	comment := "work\u202ekey\u202c"
	identities := []identity.Identity{{
		Comment: comment, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:key",
	}}
	table := FormatIdentityTable(identities)
	if !strings.Contains(table, "| work key ") || strings.ContainsAny(table, "\u202e\u202c") {
		t.Fatalf("identity table did not sanitize its comment: %q", table)
	}
	if got := identities[0].Comment; got != comment {
		t.Fatalf("display formatting changed the original agent comment to %q", got)
	}
}

func TestIdentityViewsSanitizeAllDisplayMetadata(t *testing.T) {
	const injected = "\x1b]52;c;ZXZpbA==\a"
	ids := []identity.Identity{{Comment: "key", Algorithm: injected, Fingerprint: injected}}
	options := makeIdentityOptions(ids)
	for _, output := range []string{
		FormatIdentityTable(ids),
		tuiRenderSelectionFrame(options, matchIdentities(options, ""), "", 0, 100, SelectionContext{}, time.Now()),
	} {
		if strings.Contains(output, "\x1b]52;") || strings.ContainsRune(output, '\a') {
			t.Fatalf("terminal control injection survived display formatting: %q", output)
		}
	}
}

func TestFormatIdentityTableBoundsLongCommentExpansion(t *testing.T) {
	blob, err := hex.DecodeString("0000000b7373682d65643235353139000000200000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	id, err := identity.New(blob, []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]identity.Identity, 256)
	for i := range ids {
		ids[i] = id
	}
	ids[0].Comment = strings.Repeat("あ", 22000)
	message, err := protocol.MarshalIdentities(ids)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := protocol.ParseIdentities(message)
	if err != nil {
		t.Fatal(err)
	}
	table := FormatIdentityTable(parsed)
	if len(table) > 64*1024 || !strings.Contains(table, "…") {
		t.Fatalf("%d-byte agent response expanded to a %d-byte table without bounded columns", len(message), len(table))
	}
	if parsed[0].Comment != ids[0].Comment {
		t.Fatal("display truncation changed the original agent comment")
	}
}
