//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// guiDialogEdgePadding gives macOS sheet contents room around their rounded window corners.
func guiDialogEdgePadding(theme *ui.Theme) float32 {
	if runtime.GOOS == "darwin" {
		return theme.Space(4)
	}
	return theme.Space(2)
}

// guiDialogActionRow gives dialog footers one shared button gap and alignment.
func guiDialogActionRow(c *ui.Context, theme *ui.Theme, justify ui.Align, children func()) ui.Element {
	row := ui.Row(c).FillWidth().Gap(theme.Space(2)).AlignItems(ui.Center).Justify(justify)
	row.Children(children)
	return row
}

// guiDialogActionFooter centers its actions in the remaining vertical space.
func guiDialogActionFooter(c *ui.Context, theme *ui.Theme, children func()) ui.Element {
	footer := ui.Column(c).Grow(1).Center()
	footer.Children(func() { guiDialogActionRow(c, theme, ui.End, children) })
	return footer
}

// guiDialogActionButton uses MyGo's standard button face and keeps its label centered.
func guiDialogActionButton(c *ui.Context, label string, primary, disabled bool) ui.Element {
	var button ui.Element
	if primary {
		button = ui.PrimaryButton(c, label)
	} else {
		button = ui.Button(c, label)
	}
	return button.Center().Disabled(disabled)
}
