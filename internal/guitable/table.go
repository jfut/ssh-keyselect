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

// CompactTheme gives both windows the same compact light theme while retaining MyGo's default font.
func CompactTheme(c *ui.Context) *ui.Theme {
	compact := *c.Theme()
	compact.Dark = false
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
	compact.Spacing = compactSpacing
	c.SetTheme(&compact)
	return c.Theme()
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
		cell.Background(ui.Hex("#edf3fa"))
	}
	cell.Children(content)
}
