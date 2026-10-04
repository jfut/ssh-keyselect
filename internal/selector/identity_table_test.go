// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/identity"
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
