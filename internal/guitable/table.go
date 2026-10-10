//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package guitable shares compact MyGo styling and builds the SSH identity table used by both windows.
package guitable

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/jfut/ssh-keyselect/internal/identity"
)

// compactSpacing keeps MyGo's minimum table row and header heights consistent across both windows.
const compactSpacing = 3

// CompactTheme applies the shared compact palette while retaining MyGo's current theme and default font.
func CompactTheme(c *ui.Context) *ui.Theme {
	compact := *c.Theme()
	if compact.Dark {
		compact.Background = ui.Hex("#1e1e1e")
		compact.Surface = ui.Hex("#252526")
		compact.SurfaceHover = ui.Hex("#2a2d2e")
		compact.SurfacePressed = ui.Hex("#37373d")
		compact.Border = ui.Hex("#3c3c3c")
		compact.Text = ui.Hex("#cccccc")
		compact.TextMuted = ui.Hex("#a0a0a0")
		compact.Accent = ui.Hex("#007acc")
		compact.AccentHover = ui.Hex("#1177bb")
		compact.AccentPressed = ui.Hex("#005a9e")
		compact.AccentText = ui.Hex("#ffffff")
		compact.Danger = ui.Hex("#f48771")
		compact.Success = ui.Hex("#89d185")
		compact.Warning = ui.Hex("#cca700")
		compact.Selection = ui.Hex("#094771")
		compact.Focus = ui.RGBA(0, 127, 212, 0.75)
		compact.Inverse = ui.Hex("#333333")
		compact.InverseText = ui.Hex("#cccccc")
	} else {
		compact.Background = ui.Hex("#f5f8fc")
		compact.Surface = ui.Hex("#ffffff")
		compact.SurfaceHover = ui.Hex("#f1f6fc")
		compact.SurfacePressed = ui.Hex("#e5effb")
		compact.Border = ui.Hex("#dce4ee")
		compact.Text = ui.Hex("#1c2738")
		compact.TextMuted = ui.Hex("#687586")
		compact.Accent = ui.Hex("#1870de")
		compact.AccentHover = ui.Hex("#1264ca")
		compact.AccentPressed = ui.Hex("#1057b2")
		compact.AccentText = ui.Hex("#ffffff")
		compact.Success = ui.Hex("#16804a")
		compact.Warning = ui.Hex("#c96a00")
		compact.Focus = ui.RGBA(24, 112, 222, 0.45)
	}
	compact.Spacing = compactSpacing
	c.SetTheme(&compact)
	return c.Theme()
}

// AccentBadgeColors returns readable accent badge colors for the active theme.
func AccentBadgeColors(theme *ui.Theme) (ui.Color, ui.Color) {
	if theme.Dark {
		return ui.Hex("#094771"), ui.Hex("#9cdcfe")
	}
	return ui.Hex("#e8f2ff"), ui.Hex("#1870de")
}

// SuccessBadgeColors returns readable success badge colors for the active theme.
func SuccessBadgeColors(theme *ui.Theme) (ui.Color, ui.Color) {
	if theme.Dark {
		return ui.Hex("#1e3a29"), ui.Hex("#89d185")
	}
	return ui.Hex("#e6f8ee"), ui.Hex("#16804a")
}

// WarningBadgeColors returns readable warning badge colors for the active theme.
func WarningBadgeColors(theme *ui.Theme) (ui.Color, ui.Color) {
	if theme.Dark {
		return ui.Hex("#433a1d"), ui.Hex("#cca700")
	}
	return ui.Hex("#fff6e0"), ui.Hex("#b56b00")
}

// DangerBadgeColors returns readable danger badge colors for the active theme.
func DangerBadgeColors(theme *ui.Theme) (ui.Color, ui.Color) {
	if theme.Dark {
		return ui.Hex("#5a1d1d"), ui.Hex("#f48771")
	}
	return ui.Hex("#b62b2b"), ui.Hex("#ffffff")
}

// IdentityRow holds an identity and its displayed number. A zero number uses the row's current position.
type IdentityRow struct {
	Identity identity.Identity
	Number   int
}

// IdentityTable builds the common key table, including optional row numbers and sorting.
func IdentityTable(c *ui.Context, state *ui.ListState, rows []IdentityRow, numbered, sortable bool) ui.Element {
	theme := c.Theme()
	columns := identityColumns(numbered, sortable)
	selected := -1
	if state != nil {
		if state.Selected != nil {
			selected = *state.Selected
		}
		state.Key = func(row int) any { return identity.Digest(rows[row].Identity.Blob) }
	}
	tableTheme := *theme
	if tableTheme.Dark {
		tableTheme.Accent = ui.Hex("#04395e")
		tableTheme.AccentText = ui.Hex("#ffffff")
	}
	c.SetTheme(&tableTheme)
	table := ui.Table(c, state, columns, len(rows), func(row, column int) {
		entry := rows[row]
		IdentityCell(c, row, selected, theme, func() {
			switch columns[column].ID {
			case "number":
				number := entry.Number
				if number == 0 {
					number = row + 1
				}
				ui.Text(c, strconv.Itoa(number)).SingleLine().Grow(1).TextAlign(ui.End)
			case "comment":
				comment := identity.DisplayText(entry.Identity.Comment)
				if comment == "" {
					comment = "(no comment)"
				}
				ui.Text(c, comment).SingleLine()
			case "type":
				ui.Text(c, identity.DisplayText(entry.Identity.Algorithm)).SingleLine()
			case "size":
				ui.Text(c, identity.DisplayBitSize(entry.Identity.Blob, entry.Identity.Algorithm)).
					SingleLine().Grow(1).TextAlign(ui.End)
			case "fingerprint":
				ui.Text(c, identity.DisplayText(entry.Identity.Fingerprint)).SingleLine()
			}
		})
	})
	c.SetTheme(theme)
	// MyGo's table headers inherit the table font size. Cells set their own
	// compact size, so reducing the table size affects only the headers.
	return table.FontSize(theme.Rem(1) - 1)
}

// IdentityCopyText formats the visible identity fields for the table's clipboard action.
func IdentityCopyText(id identity.Identity) string {
	comment := identity.DisplayText(id.Comment)
	if comment == "" {
		comment = "(no comment)"
	}
	return strings.Join([]string{
		comment,
		identity.DisplayText(id.Algorithm),
		identity.DisplayBitSize(id.Blob, id.Algorithm),
		identity.DisplayText(id.Fingerprint),
	}, "\t")
}

// identityColumns returns the shared layout, optionally prefixed with its row number.
func identityColumns(numbered, sortable bool) []ui.TableColumn {
	columns := []ui.TableColumn{
		{ID: "comment", Title: "Comment", Width: 240, MinWidth: 160, Sortable: sortable},
		{ID: "type", Title: "Type", Width: 96, MinWidth: 84, Sortable: sortable},
		{ID: "size", Title: "Size", Width: 58, MinWidth: 52, Align: ui.End, Sortable: sortable},
		{ID: "fingerprint", Title: "Fingerprint", MinWidth: 180, Sortable: sortable},
	}
	if numbered {
		columns = append([]ui.TableColumn{{ID: "number", Title: "No", Width: 40, MinWidth: 36, Align: ui.End, Sortable: sortable}}, columns...)
	}
	return columns
}

// IdentityCell applies the compact type and alternating row colors in both key tables.
func IdentityCell(c *ui.Context, row, selected int, theme *ui.Theme, content func()) {
	cell := ui.Row(c).Grow(1).
		Margin(-theme.Space(1.5), -theme.Space(2.5)).
		Padding(theme.Space(1.5), theme.Space(2.5)).FontSize(theme.Rem(0.9))
	if row%2 == 1 && row != selected {
		if theme.Dark {
			cell.Background(ui.Hex("#2a2d2e"))
		} else {
			cell.Background(ui.Hex("#edf3fa"))
		}
	}
	cell.Children(content)
}
