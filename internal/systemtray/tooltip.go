// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package systemtray

import (
	"fmt"

	"github.com/jfut/ssh-keyselect/internal/branding"
)

// trayTooltip labels concurrent tray instances without numbering the first one.
func trayTooltip(slot uint32) string {
	return branding.Name + trayTitleSuffix(slot)
}

// trayTitleSuffix numbers additional instances consistently with their tray tooltip.
func trayTitleSuffix(slot uint32) string {
	if slot <= 1 {
		return ""
	}
	return fmt.Sprintf(" (%d)", slot)
}
