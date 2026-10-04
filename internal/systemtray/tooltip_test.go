// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package systemtray

import "testing"

func TestTrayTooltipIncludesListenEndpoint(t *testing.T) {
	const endpoint = "C:¥Users¥jun¥.ssh¥ssh-keyselect-agent.sock"
	if got, want := trayTooltip(endpoint), endpoint+" - SSH KeySelect"; got != want {
		t.Fatalf("tray tooltip = %q, want %q", got, want)
	}
}
