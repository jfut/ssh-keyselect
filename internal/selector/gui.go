//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/guiidentitytable"
	"github.com/jfut/ssh-keyselect/internal/guistyle"
	"github.com/jfut/ssh-keyselect/internal/guiwindow"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/upstream"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/behavior"
	"github.com/richardwilkes/unison/enums/mod"
)

// GUISelector shows a floating Unison window for each identity choice.
type GUISelector struct {
	queue              chan struct{}
	stopped            chan struct{}
	initOnce           sync.Once
	stopOnce           sync.Once
	callbackMu         sync.RWMutex
	refresh            func(context.Context) ([]identity.Identity, error)
	selectionDismissed func()
}

// NewGUISelector creates a selector that must be used after Unison.Start begins.
func NewGUISelector() *GUISelector {
	return &GUISelector{queue: make(chan struct{}, 1), stopped: make(chan struct{})}
}

// SetRefreshCallback supplies the upstream identity listing used by the picker's Refresh Keys button.
func (s *GUISelector) SetRefreshCallback(refresh func(context.Context) ([]identity.Identity, error)) {
	s.callbackMu.Lock()
	s.refresh = refresh
	s.callbackMu.Unlock()
}

// SetSelectionDismissedCallback runs callback on the UI thread after a picker closes.
func (s *GUISelector) SetSelectionDismissedCallback(callback func()) {
	s.callbackMu.Lock()
	s.selectionDismissed = callback
	s.callbackMu.Unlock()
}

func (s *GUISelector) refreshCallback() func(context.Context) ([]identity.Identity, error) {
	s.callbackMu.RLock()
	defer s.callbackMu.RUnlock()
	return s.refresh
}

func (s *GUISelector) selectionDismissedCallback() func() {
	s.callbackMu.RLock()
	defer s.callbackMu.RUnlock()
	return s.selectionDismissed
}

// Stop cancels pending selection requests when the GUI application is closing.
func (s *GUISelector) Stop() {
	s.init()
	s.stopOnce.Do(func() { close(s.stopped) })
}

// Select displays the accepted SSH host-key path alongside the identity picker.
func (s *GUISelector) Select(ctx context.Context, identities []identity.Identity, requestContext SelectionContext) ([]identity.Identity, error) {
	if len(identities) == 0 {
		return nil, nil
	}
	s.init()
	select {
	case <-s.stopped:
		return nil, ErrCancelled
	default:
	}
	select {
	case s.queue <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.stopped:
		return nil, ErrCancelled
	}
	defer func() { <-s.queue }()

	result := make(chan guiSelectionResult, 1)
	unison.InvokeTask(func() {
		if err := ctx.Err(); err != nil {
			result <- guiSelectionResult{err: err}
			return
		}
		select {
		case <-s.stopped:
			result <- guiSelectionResult{err: ErrCancelled}
			return
		default:
		}
		previousForeground := capturePreviousForegroundWindow()
		shownAt := time.Now()
		window, chosenIdentity, err := guiNewSelectionWindow(ctx, identities, s.refreshCallback(), requestContext, shownAt)
		if err != nil {
			result <- guiSelectionResult{err: err}
			return
		}
		stopClose := context.AfterFunc(ctx, func() {
			unison.InvokeTask(func() {
				if window.IsValid() {
					window.StopModal(unison.ModalResponseCancel)
				}
			})
		})
		response := window.RunModal()
		stopClose()
		if callback := s.selectionDismissedCallback(); callback != nil {
			callback()
		}
		selectedIdentity, selected := chosenIdentity()
		if err := ctx.Err(); err != nil {
			result <- guiSelectionResult{err: err}
			return
		}
		if response != unison.ModalResponseOK || !selected {
			result <- guiSelectionResult{err: ErrCancelled}
			return
		}
		// RunModal reactivates the app's previously active window as it unwinds. Queue external focus restoration
		// for the next event pass so that this deferred activation cannot put the app window back on top afterward.
		unison.InvokeTask(func() {
			restorePreviousForegroundWindow(previousForeground)
			result <- guiSelectionResult{identity: selectedIdentity}
		})
	})

	var selection guiSelectionResult
	select {
	case selection = <-result:
	case <-ctx.Done():
		select {
		case selection = <-result:
		case <-s.stopped:
			return nil, ErrCancelled
		}
	case <-s.stopped:
		return nil, ErrCancelled
	}
	if selection.err != nil {
		return nil, selection.err
	}
	return []identity.Identity{selection.identity}, nil
}

func (s *GUISelector) init() {
	s.initOnce.Do(func() {
		if s.queue == nil {
			s.queue = make(chan struct{}, 1)
		}
		if s.stopped == nil {
			s.stopped = make(chan struct{})
		}
	})
}

type guiSelectionResult struct {
	identity identity.Identity
	err      error
}

func guiNewSelectionWindow(
	ctx context.Context,
	identities []identity.Identity,
	refreshIdentities func(context.Context) ([]identity.Identity, error),
	requestContext SelectionContext,
	shownAt time.Time,
) (*unison.Window, func() (identity.Identity, bool), error) {
	title := fmt.Sprintf("%s [%s - %s]", identitySelectionPrompt, branding.Name, selectionDisplayTime(shownAt))
	window, err := unison.NewWindow(title, unison.FloatingWindowOption(), unison.NotResizableWindowOption())
	if err != nil {
		return nil, nil, fmt.Errorf("create identity selection window: %w", err)
	}
	if icons, iconErr := guiassets.TitleIcons(); iconErr == nil {
		window.SetTitleIcons(icons)
	}
	content := window.Content()
	content.SetBorder(unison.NewEmptyBorder(geom.NewUniformInsets(8)))
	content.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 5})

	fingerprintFont := guistyle.MonospacedFont(9)
	mutedInk := unison.RGB(104, 117, 134)
	rowInk := unison.RGB(255, 255, 255)
	detailsScroll := guiNewSelectionDetailsArea(window, requestContext, shownAt, rowInk)
	content.AddChild(detailsScroll)

	tableView := guiidentitytable.New(true)
	tableView.SetSortable(false)
	scroller := unison.NewScrollPanel()
	tableView.AttachTo(scroller)
	scroller.SetLayoutData(guiidentitytable.ScrollLayoutData(len(identities), 7))
	content.AddChild(scroller)

	footer := unison.NewPanel()
	footer.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 8, VAlign: align.Middle})
	footer.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
	status := unison.NewLabel()
	status.Font = fingerprintFont
	status.OnBackgroundInk = mutedInk
	status.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true, VAlign: align.Middle})
	footer.AddChild(status)

	refreshButton := guistyle.NewRefreshButton()
	refreshButton.SetEnabled(refreshIdentities != nil)
	footer.AddChild(refreshButton)
	content.AddChild(footer)

	filter := unison.NewField()
	filter.Font = fingerprintFont
	filter.Watermark = "> " + identitySelectionFilterHint
	filter.BackgroundInk = rowInk
	filter.EditableInk = rowInk
	filter.OnBackgroundInk = unison.RGB(28, 39, 56)
	filter.OnEditableInk = unison.RGB(28, 39, 56)
	filter.SelectionInk = unison.ThemeFocus
	filter.OnSelectionInk = unison.ThemeOnFocus
	unison.InstallFocusBorders(filter, filter,
		unison.NewCompoundBorder(
			unison.NewLineBorder(unison.ThemeFocus, geom.Size{}, geom.NewUniformInsets(1), false),
			unison.NewEmptyBorder(geom.Insets{Top: 1, Left: 2, Bottom: 1, Right: 2}),
		),
		unison.NewCompoundBorder(
			unison.NewLineBorder(unison.ThemeSurfaceEdge, geom.Size{}, geom.NewUniformInsets(1), false),
			unison.NewEmptyBorder(geom.Insets{Top: 1, Left: 2, Bottom: 1, Right: 2}),
		),
	)
	filter.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
	content.AddChild(filter)

	options := makeSearchableIdentityOptions(identities)
	offeredIdentities := append([]identity.Identity(nil), identities...)
	matches := matchIdentities(options, "")
	var chosen identity.Identity
	hasChosen := false
	active := true
	refreshChoices := func(query string) {
		matches = matchIdentities(options, query)
		entries := make([]guiidentitytable.Entry, 0, len(matches))
		for _, match := range matches {
			entries = append(entries, guiidentitytable.Entry{Identity: match.identity, Number: match.index + 1})
		}
		tableView.SetEntries(entries)
		if len(matches) > 0 {
			tableView.Table.SetLeadCell(0, -1)
		}
		status.SetTitle(fmt.Sprintf("%d/%d", len(matches), len(identities)))
		status.Tooltip = nil
		tableView.Table.MarkForLayoutAndRedraw()
		scroller.MarkForLayoutAndRedraw()
	}
	moveSelection := func(delta int) {
		if len(matches) == 0 {
			return
		}
		selected := tableView.Table.LeadRowIndex()
		if selected < 0 || selected >= len(matches) {
			selected = 0
		} else {
			selected = (selected + delta + len(matches)) % len(matches)
		}
		tableView.Table.SetLeadCell(selected, -1)
	}
	selectCurrent := func() {
		selected := tableView.Table.LeadRowIndex()
		if selected >= 0 && selected < len(matches) {
			chosen = matches[selected].identity
			hasChosen = true
			window.StopModal(unison.ModalResponseOK)
		}
	}
	tableView.Table.DoubleClickCallback = selectCurrent
	tableKeyDown := tableView.Table.KeyDownCallback
	tableView.Table.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		switch keyCode {
		case unison.KeyReturn, unison.KeyNumPadEnter:
			selectCurrent()
			return true
		case unison.KeyEscape:
			window.StopModal(unison.ModalResponseCancel)
			return true
		default:
			return tableKeyDown(keyCode, modifiers, repeat)
		}
	}
	filter.ModifiedCallback = func(_, after *unison.FieldState) { refreshChoices(after.Text) }
	filter.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		switch keyCode {
		case unison.KeyUp:
			moveSelection(-1)
			return true
		case unison.KeyDown:
			moveSelection(1)
			return true
		case unison.KeyReturn, unison.KeyNumPadEnter:
			selectCurrent()
			return true
		case unison.KeyEscape:
			window.StopModal(unison.ModalResponseCancel)
			return true
		case unison.KeyU:
			if modifiers.ControlDown() {
				filter.SetText("")
				return true
			}
			return filter.DefaultKeyDown(keyCode, modifiers, repeat)
		case unison.KeyC:
			if modifiers.OSMenuCommandDown() && tableView.CopySelection() {
				return true
			}
			return filter.DefaultKeyDown(keyCode, modifiers, repeat)
		default:
			return filter.DefaultKeyDown(keyCode, modifiers, repeat)
		}
	}
	refreshChoices("")
	refreshButton.ClickCallback = func() {
		if refreshIdentities == nil {
			return
		}
		refreshButton.SetEnabled(false)
		go func() {
			refreshCtx, cancel := context.WithTimeout(ctx, upstream.RequestTimeout)
			defer cancel()
			updated, refreshErr := refreshIdentities(refreshCtx)
			unison.InvokeTask(func() {
				if !window.IsValid() || !active {
					return
				}
				refreshButton.SetEnabled(true)
				if refreshErr != nil {
					status.Tooltip = unison.NewTooltipWithText(refreshErr.Error())
					status.MarkForRedraw()
					return
				}
				status.Tooltip = nil
				identities = guiAvailableIdentityMetadata(offeredIdentities, updated)
				options = makeSearchableIdentityOptions(identities)
				refreshChoices(filter.Text())
				scroller.SetLayoutData(guiidentitytable.ScrollLayoutData(len(identities), 7))
				contentRect := window.ContentRect()
				contentRect.Height = guiSelectionWindowHeight(len(identities), fingerprintFont, requestContext)
				window.SetContentRect(contentRect)
				guiwindow.CenterOnPrimaryDisplay(window)
			})
		}()
	}

	window.SetContentRect(geom.NewRect(0, 0, 820, guiSelectionWindowHeight(len(identities), fingerprintFont, requestContext)))
	content.ValidateLayout()
	detailsScroll.SetPosition(0, math.MaxFloat32)
	guiwindow.CenterOnPrimaryDisplay(window)
	filter.RequestFocus()
	return window, func() (identity.Identity, bool) {
		active = false
		return chosen, hasChosen
	}, nil
}

func guiNewSelectionDetailsArea(window *unison.Window, requestContext SelectionContext, shownAt time.Time, rowInk unison.Ink) *unison.ScrollPanel {
	textInk := unison.RGB(28, 39, 56)
	detailsFont := guiSelectionDetailsFont()

	detailsField := unison.NewMultiLineField()
	detailsField.Font = detailsFont
	detailsField.SetText(guiSelectionDetailsText(requestContext, shownAt))
	detailsField.BackgroundInk = rowInk
	detailsField.OnBackgroundInk = textInk
	detailsField.EditableInk = rowInk
	detailsField.OnEditableInk = textInk
	detailsField.SelectionInk = unison.ThemeFocus
	detailsField.OnSelectionInk = unison.ThemeOnFocus
	guistyle.MakeFieldReadOnly(detailsField)
	readOnlyKeyDown := detailsField.KeyDownCallback
	detailsField.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		if keyCode == unison.KeyEscape {
			window.StopModal(unison.ModalResponseCancel)
			return true
		}
		return readOnlyKeyDown(keyCode, modifiers, repeat)
	}
	// Keep the field selectable and copyable while making its contents read-only.
	unison.UninstallFocusBorders(detailsField, detailsField)
	detailsField.SetBorder(unison.NewEmptyBorder(geom.NewUniformInsets(5)))

	detailsScroll := unison.NewScrollPanel()
	detailsScroll.BackgroundInk = rowInk
	detailsScroll.SetBorder(unison.NewLineBorder(unison.RGB(220, 228, 238), geom.NewUniformSize(8), geom.NewUniformInsets(1), false))
	detailsScroll.SetContent(detailsField, behavior.HintedFill, behavior.Fill)
	detailsScroll.SetLayoutData(&unison.FlexLayoutData{
		HAlign: align.Fill, VAlign: align.Fill, HGrab: true, SizeHint: geom.NewSize(780, guiSelectionDetailsTextAreaHeight(requestContext)),
	})
	return detailsScroll
}

func guiSelectionDetailsText(requestContext SelectionContext, shownAt time.Time) string {
	lines := selectionTreeLines(requestContext)
	var text strings.Builder
	fmt.Fprintf(&text, "[%s - %s]\n", branding.Name, selectionDisplayTime(shownAt))
	for index, line := range lines {
		text.WriteString(line.prefix)
		text.WriteString(line.label)
		if line.hasValue {
			text.WriteString(": ")
			text.WriteString(line.value)
		}
		if index < len(lines)-1 {
			text.WriteByte('\n')
		}
	}
	return text.String()
}

func guiSelectionDetailsTextAreaHeight(requestContext SelectionContext) float32 {
	return float32(guiSelectionDetailsVisibleLines(requestContext)+1)*guiSelectionDetailsFont().LineHeight() + 12
}

func guiSelectionDetailsFont() unison.Font {
	return guistyle.MonospacedFont(8.5)
}

func guiSelectionDetailsVisibleLines(requestContext SelectionContext) int {
	return min(guiSelectionDetailsLineCount(requestContext), 5)
}

func guiSelectionDetailsLineCount(requestContext SelectionContext) int {
	return len(selectionTreeLines(requestContext))
}

func guiSelectionWindowHeight(identityCount int, filterFont unison.Font, requestContext SelectionContext) float32 {
	visibleRows := min(max(identityCount, 1), 7)
	return float32(16) + guiSelectionDetailsTextAreaHeight(requestContext) + 5 + guiidentitytable.HeaderHeight +
		float32(visibleRows)*guiidentitytable.RowHeight + 5 + 18 + 5 + filterFont.LineHeight() + 10
}

// guiAvailableIdentityMetadata refreshes display data without selecting keys the SSH client was not offered.
func guiAvailableIdentityMetadata(offered, current []identity.Identity) []identity.Identity {
	currentByDigest := make(map[[32]byte]identity.Identity, len(current))
	for _, id := range current {
		currentByDigest[identity.Digest(id.Blob)] = id
	}
	available := make([]identity.Identity, 0, min(len(offered), len(current)))
	for _, id := range offered {
		if refreshed, ok := currentByDigest[identity.Digest(id.Blob)]; ok {
			available = append(available, refreshed)
		}
	}
	return available
}

var _ Selector = (*GUISelector)(nil)
