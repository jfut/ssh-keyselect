//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package guiidentitytable provides the shared SSH identity table used by the GUI status window and picker.
package guiidentitytable

import (
	"strconv"
	"strings"

	"github.com/jfut/ssh-keyselect/internal/guistyle"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/toolbox/v2/tid"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/behavior"
	"github.com/richardwilkes/unison/enums/mod"
)

const (
	// MaxVisibleRows is the number of identity rows shown in the status window before scrolling.
	MaxVisibleRows = 5
	HeaderHeight   = float32(24)
	RowHeight      = float32(25)
)

var (
	textInk   = unison.RGB(28, 39, 56)
	mutedInk  = unison.RGB(104, 117, 134)
	cardInk   = unison.RGB(255, 255, 255)
	inputInk  = unison.RGB(247, 249, 252)
	borderInk = unison.RGB(220, 228, 238)
	headerInk = unison.RGB(241, 245, 250)
	accentInk = unison.RGB(24, 112, 222)
)

// Entry supplies an identity and its displayed number. Number is one-based when the table includes a No. column.
type Entry struct {
	Identity identity.Identity
	Number   int
}

// View bundles a table and its header so both GUI surfaces share the same row and column presentation.
type View struct {
	Table    *unison.Table[*Row]
	header   *unison.TableHeader[*Row]
	numbered bool
}

// Row adapts SSH identity metadata to Unison's table model.
type Row struct {
	table       *unison.Table[*Row]
	id          tid.TID
	numbered    bool
	number      string
	size        string
	algorithm   string
	fingerprint string
	comment     string
}

// New creates the shared table, optionally adding a No. column at the left edge.
func New(numbered bool) *View {
	table := unison.NewTable(&unison.SimpleTableModel[*Row]{})
	columns := make([]unison.ColumnInfo, 0, 5)
	if numbered {
		// Keep the picker compact while retaining enough width for key types and SHA256 fingerprints.
		columns = append(columns, unison.ColumnInfo{ID: 0, Current: 36, Minimum: 32})
		columns = append(columns,
			unison.ColumnInfo{ID: 1, Current: 230, Minimum: 160},
			unison.ColumnInfo{ID: 2, Current: 100, Minimum: 80},
			unison.ColumnInfo{ID: 3, Current: 58, Minimum: 45},
			unison.ColumnInfo{ID: 4, Current: 360, Minimum: 180},
		)
	} else {
		columns = append(columns,
			unison.ColumnInfo{ID: 0, Current: 320, Minimum: 190},
			unison.ColumnInfo{ID: 1, Current: 140, Minimum: 100},
			unison.ColumnInfo{ID: 2, Current: 76, Minimum: 60},
			unison.ColumnInfo{ID: 3, Current: 390, Minimum: 220},
		)
	}
	table.Columns = columns
	table.BackgroundInk = cardInk
	table.OnBackgroundInk = textInk
	table.BandingInk = inputInk
	table.OnBandingInk = textInk
	table.SelectionInk = accentInk
	table.OnSelectionInk = cardInk
	table.InactiveSelectionInk = accentInk
	table.OnInactiveSelectionInk = cardInk
	table.Padding = geom.Insets{Top: 3, Left: 9, Bottom: 3, Right: 9}
	table.MinimumRowHeight = RowHeight
	table.ShowRowDivider = true
	table.ShowColumnDivider = false
	table.InteriorDividerInk = borderInk

	headers := make([]*unison.DefaultTableColumnHeader[*Row], 0, len(columns))
	if numbered {
		headers = append(headers, unison.NewTableColumnHeader[*Row]("No", "", nil))
	}
	headers = append(headers,
		unison.NewTableColumnHeader[*Row]("Comment", "", nil),
		unison.NewTableColumnHeader[*Row]("Type", "", nil),
		unison.NewTableColumnHeader[*Row]("Size", "", nil),
		unison.NewTableColumnHeader[*Row]("Fingerprint", "", nil),
	)
	columnHeaders := make([]unison.TableColumnHeader[*Row], len(headers))
	for index, columnHeader := range headers {
		columnHeaders[index] = columnHeader
	}
	header := unison.NewTableHeader(table, columnHeaders...)
	defaultHeaderSizer := header.DefaultSizes
	header.SetSizer(func(hint geom.Size) (minSize, prefSize, maxSize geom.Size) {
		minSize, prefSize, maxSize = defaultHeaderSizer(hint)
		minSize.Height = HeaderHeight
		prefSize.Height = HeaderHeight
		maxSize.Height = HeaderHeight
		return minSize, prefSize, maxSize
	})
	defaultHeaderDraw := header.DefaultDraw
	header.DrawCallback = func(canvas *unison.Canvas, dirty geom.Rect) {
		originalMinimumRowHeight := table.MinimumRowHeight
		table.MinimumRowHeight = HeaderHeight
		defer func() { table.MinimumRowHeight = originalMinimumRowHeight }()
		defaultHeaderDraw(canvas, dirty)
	}
	header.BackgroundInk = headerInk
	header.InteriorDividerColor = borderInk
	for _, columnHeader := range headers {
		columnHeader.OnBackgroundInk = mutedInk
		columnHeader.Font = guistyle.Font(10, false)
		columnHeader.VAlign = align.Middle
		// Header constructors capture font metrics, so rebuild the text after styling.
		columnHeader.SetTitle(columnHeader.String())
	}
	view := &View{Table: table, header: header, numbered: numbered}
	// Both GUI tables expose the same copy action; pickers can extend these key bindings.
	table.ContextMenuCallback = func(geom.Point) unison.Menu {
		row := view.selectedRow()
		if row == nil {
			return nil
		}
		factory := unison.DefaultMenuFactory()
		menu := factory.NewMenu(unison.PopupMenuTemporaryBaseID|unison.ContextMenuIDFlag, "", nil)
		menu.InsertItem(-1, factory.NewItem(
			unison.PopupMenuTemporaryBaseID+1|unison.ContextMenuIDFlag,
			"Copy", unison.KeyBinding{}, nil,
			func(unison.MenuItem) { unison.ClipboardSetText(row.copyText()) },
		))
		return menu
	}
	table.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		if keyCode == unison.KeyC && modifiers.OSMenuCommandDown() && view.CopySelection() {
			return true
		}
		return table.DefaultKeyDown(keyCode, modifiers, repeat)
	}
	return view
}

func (v *View) selectedRow() *Row {
	selected := v.Table.LeadRowIndex()
	rows := v.Table.RootRows()
	if selected < 0 || selected >= len(rows) {
		return nil
	}
	return rows[selected]
}

// CopySelection also lets the picker's filter field copy the current table row.
func (v *View) CopySelection() bool {
	row := v.selectedRow()
	if row == nil {
		return false
	}
	unison.ClipboardSetText(row.copyText())
	return true
}

// AttachTo installs this table and its column header in the supplied scroll panel using the status window's card edge.
func (v *View) AttachTo(scroller *unison.ScrollPanel) {
	scroller.SetContent(v.Table, behavior.Fill, behavior.Fill)
	scroller.SetColumnHeader(v.header)
	scroller.BackgroundInk = cardInk
	scroller.SetBorder(unison.NewLineBorder(borderInk, geom.NewUniformSize(8), geom.NewUniformInsets(1), false))
}

// SetSortable controls whether clicking a column header can reorder this view's rows.
func (v *View) SetSortable(sortable bool) {
	for _, header := range v.header.ColumnHeaders {
		state := header.SortState()
		state.Sortable = sortable
		header.SetSortState(state)
	}
}

// SetRows replaces the rows with identities numbered in their supplied order and sizes columns to their contents.
func (v *View) SetRows(identities []identity.Identity) {
	entries := make([]Entry, len(identities))
	for index, id := range identities {
		entries[index] = Entry{Identity: id, Number: index + 1}
	}
	v.SetEntries(entries)
	v.fitColumns()
}

// SetEntries replaces the rows without resizing columns, which keeps a filtered picker table from shifting as it updates.
func (v *View) SetEntries(entries []Entry) {
	rows := make([]*Row, 0, len(entries))
	for _, entry := range entries {
		id := entry.Identity
		comment := identity.DisplayComment(id.Comment)
		if comment == "" {
			comment = "(no comment)"
		}
		row := &Row{
			table:       v.Table,
			id:          tid.MustNewTID('k'),
			numbered:    v.numbered,
			size:        identity.DisplayBitSize(id.Blob, id.Algorithm),
			algorithm:   id.Algorithm,
			fingerprint: id.Fingerprint,
			comment:     comment,
		}
		if v.numbered {
			row.number = strconv.Itoa(entry.Number)
		}
		rows = append(rows, row)
	}
	v.Table.SetRootRows(rows)
	v.Table.MarkForLayoutAndRedraw()
}

// fitColumns sizes the non-fingerprint columns to their preferred content widths.
func (v *View) fitColumns() {
	if v.Table.Model.RootRowCount() > 0 {
		v.Table.SizeColumnsToFitWithExcessIn(v.Table.Columns[len(v.Table.Columns)-1].ID)
	}
	v.Table.MarkForLayoutAndRedraw()
}

// ScrollLayoutData caps the scroll panel at visibleRows rows while allowing shorter lists to remain compact.
func ScrollLayoutData(rowCount, visibleRows int) *unison.FlexLayoutData {
	visibleRows = max(visibleRows, 1)
	rows := max(1, min(rowCount, visibleRows))
	return &unison.FlexLayoutData{
		HAlign:   align.Fill,
		VAlign:   align.Fill,
		HGrab:    true,
		VGrab:    true,
		SizeHint: geom.NewSize(0, HeaderHeight+float32(rows)*RowHeight),
	}
}

func (r *Row) CloneForTarget(target unison.Paneler, _ *Row) *Row {
	clone := *r
	clone.table = target.(*unison.Table[*Row])
	clone.id = tid.MustNewTID('k')
	return &clone
}

func (r *Row) ID() tid.TID           { return r.id }
func (r *Row) Parent() *Row          { return nil }
func (r *Row) SetParent(*Row)        {}
func (r *Row) CanHaveChildren() bool { return false }
func (r *Row) Children() []*Row      { return nil }
func (r *Row) SetChildren([]*Row)    {}
func (r *Row) IsOpen() bool          { return false }
func (r *Row) SetOpen(bool)          {}

func (r *Row) CellDataForSort(column int) string {
	if r.numbered {
		if column == 0 {
			return r.number
		}
		column--
	}
	switch column {
	case 0:
		return r.comment
	case 1:
		return r.algorithm
	case 2:
		return r.size
	case 3:
		return r.fingerprint
	}
	return ""
}

// copyText returns the four displayed identity fields as tab-separated clipboard text.
func (r *Row) copyText() string {
	return strings.Join([]string{r.comment, r.algorithm, r.size, r.fingerprint}, "\t")
}

func (r *Row) ColumnCell(_, column int, foreground, background unison.Ink, _, _, _ bool) unison.Paneler {
	label := unison.NewLabel()
	label.OnBackgroundInk = foreground
	label.BackgroundInk = background
	label.Font = guistyle.Font(10, false)
	label.SetTitle(r.CellDataForSort(column))
	return label
}

var _ unison.TableRowData[*Row] = (*Row)(nil)
