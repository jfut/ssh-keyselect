// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package branding contains product identity and attribution shown in GUI and CLI surfaces.
package branding

const (
	// Name is the user-facing application name.
	Name = "SSH KeySelect"
	// Description briefly explains the application.
	Description = "SSH KeySelect is an interactive SSH agent proxy for per-connection key\nselection from an existing agent."
	// Subtitle describes the application's role.
	Subtitle = "Selective SSH Agent Proxy"
	// ProjectURL links to the source repository.
	ProjectURL = "https://github.com/jfut/ssh-keyselect"
	// Author is shown in the About dialog.
	Author = "Jun Futagawa (jfut)"
)

// EndpointTitle keeps the main window and tray labels consistent when settings change.
func EndpointTitle(endpoint string) string {
	if endpoint == "" {
		return Name
	}
	return endpoint + " - " + Name
}
