// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package systemtray

import "testing"

func TestTrayTooltipOmitsNumberForFirstInstance(t *testing.T) {
	if got := trayTooltip(1); got != "SSH KeySelect" {
		t.Fatalf("first instance tooltip = %q, want SSH KeySelect", got)
	}
	if got := trayTooltip(2); got != "SSH KeySelect (2)" {
		t.Fatalf("second instance tooltip = %q, want SSH KeySelect (2)", got)
	}
}
