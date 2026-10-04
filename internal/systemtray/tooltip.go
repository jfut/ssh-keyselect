// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package systemtray

import "github.com/jfut/ssh-keyselect/internal/branding"

// trayTooltip names the endpoint that this instance accepts agent requests on.
func trayTooltip(endpoint string) string {
	if endpoint == "" {
		return branding.Name
	}
	return endpoint + " - " + branding.Name
}
