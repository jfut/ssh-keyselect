//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/guitable"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

var guiPickerRefreshIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 11a8.1 8.1 0 0 0-15.5-2M4 4v5h5m-5 4a8.1 8.1 0 0 0 15.5 2M20 20v-5h-5"/></svg>`))

// GUISelector serializes identity requests and presents each one in a MyGo native window.
type GUISelector struct {
	queue    chan struct{}
	stopped  chan struct{}
	initOnce sync.Once
	stopOnce sync.Once

	mu      sync.RWMutex
	refresh func(context.Context) ([]identity.Identity, error)
	parent  func() *mygo.Window
	windows map[*mygo.Window]struct{}
}

// NewGUISelector creates a selector that is used after MyGo's application loop starts.
func NewGUISelector() *GUISelector {
	return &GUISelector{queue: make(chan struct{}, 1), stopped: make(chan struct{})}
}

// SetRefreshCallback supplies the upstream identity listing used by the picker's refresh action.
func (s *GUISelector) SetRefreshCallback(refresh func(context.Context) ([]identity.Identity, error)) {
	s.mu.Lock()
	s.refresh = refresh
	s.mu.Unlock()
}

// SetWindowProvider supplies the main window that owns each modal identity picker.
func (s *GUISelector) SetWindowProvider(parent func() *mygo.Window) {
	s.mu.Lock()
	s.parent = parent
	s.mu.Unlock()
}

// Stop cancels pending selection requests when the application is closing.
func (s *GUISelector) Stop() {
	s.init()
	s.stopOnce.Do(func() {
		close(s.stopped)
		s.mu.RLock()
		windows := make([]*mygo.Window, 0, len(s.windows))
		for window := range s.windows {
			windows = append(windows, window)
		}
		s.mu.RUnlock()
		for _, window := range windows {
			window.Close()
		}
	})
}

// Select displays the verified SSH host-key path and waits for one selected identity.
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
	select {
	case <-s.stopped:
		return nil, ErrCancelled
	default:
	}

	s.mu.RLock()
	parentProvider, refresh := s.parent, s.refresh
	s.mu.RUnlock()
	var parent *mygo.Window
	if parentProvider != nil {
		parent = parentProvider()
	}
	returnWindow := capturePickerReturnWindow()
	defer releasePickerReturnWindow(returnWindow)
	shownAt := time.Now()
	offered := append([]identity.Identity(nil), identities...)
	picker := &guiPickerState{
		ctx: ctx, identities: offered,
		offered:        offered,
		requestContext: requestContext, detailsText: guiSelectionDetailsText(requestContext, shownAt),
		shownAt: shownAt, refresh: refresh,
		selected: 0, focusFilter: true,
		// The current target is the final entry in a multi-host path.
		detailsScroll: ui.ScrollState{Y: math.MaxFloat32},
	}
	title := fmt.Sprintf("%s [%s - %s]", identitySelectionPrompt, branding.Name, selectionDisplayTime(shownAt))
	windowParent, windowModal := pickerWindowOwnership(parent)
	// Windows dimensions are the default; adjust other platforms to show five key
	// rows.
	height, minHeight := 386, 360
	switch runtime.GOOS {
	case "darwin":
		height = 380
	case "linux":
		height, minHeight = 340, 330
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: title, Parent: windowParent, Modal: windowModal, AlwaysOnTop: true,
		Width: 860, Height: height, MinWidth: 820, MinHeight: minHeight, Hidden: true,
		Content: ui.View(picker.view),
	})
	if window.IsDestroyed() {
		return nil, ErrCancelled
	}
	picker.window = window
	closed := make(chan guiSelectionResult, 1)
	focusStopped := make(chan struct{})
	focusFilter := func() {
		window.Update(func() {
			picker.focusFilter = true
			acquirePickerNativeFocus(window)
		})
	}
	window.OnClosed(func() {
		close(focusStopped)
		s.mu.Lock()
		delete(s.windows, window)
		s.mu.Unlock()
		closed <- picker.result()
	})
	window.OnFocus(focusFilter)
	if icon, err := guiassets.WindowIconPNG(); err == nil {
		_ = window.SetIcon(icon)
	}
	s.mu.Lock()
	select {
	case <-s.stopped:
		s.mu.Unlock()
		window.Close()
		return nil, ErrCancelled
	default:
		s.windows[window] = struct{}{}
		s.mu.Unlock()
	}
	// Show the modal as soon as it is created so its parent cannot stay blocked
	// if the platform has already delivered MyGo's one-shot ready event.
	window.Show()
	window.Focus()
	focusFilter()
	// The native window may become active before its UI surface receives focus.
	// Reapply the filter focus briefly instead of treating window activation as
	// proof that the input field is ready for keyboard input.
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(750 * time.Millisecond)
		defer timeout.Stop()
		for {
			select {
			case <-focusStopped:
				return
			case <-timeout.C:
				return
			case <-ticker.C:
				window.Focus()
				focusFilter()
			}
		}
	}()
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-focusStopped:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					window.Invalidate()
				}
			}
		}()
	}
	var selection guiSelectionResult
	closedByUser := false
	select {
	case selection = <-closed:
		closedByUser = true
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// The SSH request expires before the server's grace period, but keep
			// the window visible so the user can see the timeout and dismiss it.
			window.Update(func() {
				picker.expired = true
				picker.err = "Timed out. Close and retry SSH."
			})
			return nil, ctx.Err()
		}
		window.Close()
		selection = <-closed
	case <-s.stopped:
		window.Close()
		selection = <-closed
	}
	if closedByUser && ctx.Err() == nil {
		select {
		case <-s.stopped:
		default:
			// A native window manager may reactivate the GUI owner when the picker closes, so restore the SSH caller afterward.
			mygo.RunOnMain(func() { restorePickerReturnWindow(returnWindow, selection.focusTime) })
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-s.stopped:
		return nil, ErrCancelled
	default:
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
		if s.windows == nil {
			s.windows = make(map[*mygo.Window]struct{})
		}
	})
}

type guiSelectionResult struct {
	identity  identity.Identity
	focusTime uint32
	err       error
}

type guiPickerState struct {
	ctx            context.Context
	identities     []identity.Identity
	offered        []identity.Identity
	requestContext SelectionContext
	detailsText    string
	shownAt        time.Time
	refresh        func(context.Context) ([]identity.Identity, error)
	selected       int
	query          string
	lastQuery      string
	err            string
	refreshing     bool
	expired        bool
	hasSelection   bool
	chosen         identity.Identity
	focusTime      uint32
	closePending   bool
	searchOptions  []identityOption
	optionsValid   bool
	matchesCache   []identityMatch
	matchesQuery   string
	matchesValid   bool
	tableRows      []guitable.IdentityRow
	detailsScroll  ui.ScrollState
	tableState     ui.ListState
	window         *mygo.Window
	focusFilter    bool
}

func (p *guiPickerState) view(c *ui.Context) {
	theme := guitable.CompactTheme(c)
	keyBadgeFill, keyBadgeText := guitable.AccentBadgeColors(theme)
	timeoutBadgeFill, timeoutBadgeText := guitable.AccentBadgeColors(theme)
	outerPadding := theme.Space(2)
	if runtime.GOOS == "darwin" {
		// Keep picker controls clear of the rounded corners of macOS sheets.
		outerPadding = theme.Space(4)
	}
	root := ui.Column(c).Fill().Padding(outerPadding).Gap(theme.Space(1.5))
	if root.Shortcut(0, ui.KeyEscape) {
		p.closeWindow()
	}
	root.Children(func() {
		ui.Scroll(c).Height(theme.Space(36)).TrackScroll(&p.detailsScroll).
			Border(1, theme.Border).Radius(theme.Space(1)).Padding(theme.Space(1.5)).Children(func() {
			ui.Text(c, p.detailsText).FontSize(theme.Rem(0.82)).
				Selectable()
		})
		if p.lastQuery != p.query {
			// A new result set must not inherit the old selection anchor; retain only column layout.
			columns := p.tableState.Columns
			p.tableState = ui.ListState{Columns: columns}
			p.selected = 0
			// Start each new result set at its first key, including after an empty result.
			p.tableState.ScrollTo(0, ui.Start)
			p.lastQuery = p.query
		}
		matches := p.matches()
		if len(matches) == 0 {
			p.selected = -1
		}
		// Match the main window's key card around the picker table.
		tableArea := ui.Column(c).Grow(1).MinHeight(theme.Space(37)).Padding(theme.Space(2.5)).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Space(3))
		// Keep selected row highlights inside the rounded key card on every OS.
		tableArea.Clip()
		tableArea.Children(func() {
			if len(matches) == 0 {
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, "No SSH keys match this filter.").TextColor(theme.TextMuted)
				})
			} else {
				p.tableState.Selected = &p.selected
				table := guitable.IdentityTable(c, &p.tableState, p.tableRows, true, false).
					Grow(1).MinHeight(theme.Space(37))
				table.ContextMenu(func(menu *ui.Menu) {
					copyItem := menu.Item("Copy").Disabled(p.selected < 0 || p.selected >= len(matches)).
						Shortcut(ui.Cmd, ui.KeyC)
					if copyItem.Chosen() && p.selected >= 0 && p.selected < len(matches) {
						mygo.Clipboard.WriteText(guitable.IdentityCopyText(matches[p.selected].identity))
					}
				})
				if table.Shortcut(ui.Cmd, ui.KeyC) && p.selected >= 0 && p.selected < len(matches) {
					mygo.Clipboard.WriteText(guitable.IdentityCopyText(matches[p.selected].identity))
				}
				if table.Submitted() && !p.expired {
					p.choose(matches)
				}
			}
		})
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			// Match the main window's Keys badge beside the picker count.
			ui.Text(c, "Keys").FontSize(theme.Rem(0.9)).TextColor(keyBadgeText).
				Background(keyBadgeFill).Padding(theme.Space(0.5), theme.Space(1.5)).Radius(theme.Space(1.5))
			ui.Text(c, fmt.Sprintf("%d/%d", len(matches), len(p.identities))).FontSize(theme.Rem(0.9)).TextColor(theme.TextMuted)
			if p.err != "" {
				ui.Text(c, p.err).SingleLine().TextColor(theme.Danger).Grow(1)
			} else {
				ui.Spacer(c)
			}
			if seconds, ok := selectionRemainingSeconds(p.ctx); ok {
				// Keep the timeout label and its value together so the digits cannot be clipped.
				timeout := ui.Row(c).Gap(theme.Space(0.5)).AlignItems(ui.Center).Shrink(0)
				timeout.Children(func() {
					ui.Text(c, "Timeout").SingleLine().Shrink(0).FontSize(theme.Rem(0.9)).TextColor(timeoutBadgeText).
						Background(timeoutBadgeFill).Padding(theme.Space(0.5), theme.Space(1.5)).Radius(theme.Space(1.5))
					ui.Text(c, fmt.Sprintf("%ds", seconds)).NoWrap().Width(theme.Space(14)).TextAlign(ui.End).Shrink(0).
						FontSize(theme.Rem(0.9)).TextColor(theme.TextMuted).Padding(0, theme.Space(1.5))
				})
			}
			refresh := ui.PrimaryButton(c, "").Label("Refresh keys").Tooltip("Refresh keys").
				Padding(0).Size(theme.Space(7), theme.Space(7)).Disabled(p.refresh == nil || p.refreshing || p.expired || p.ctx.Err() != nil)
			refresh.Children(func() {
				ui.Icon(c, guiPickerRefreshIcon).Size(theme.Space(4), theme.Space(4))
			})
			if refresh.Clicked() {
				p.refreshKeys()
			}
		})
		filter := ui.TextInput(c, &p.query).Label("Filter").Placeholder(identitySelectionFilterHint)
		filter.AutoFocus()
		if p.focusFilter {
			filter.Focus()
			p.focusFilter = false
		}
		// Keep the filter focused while navigation keys move through matching keys.
		filter.HandleInput(func(event ui.InputEvent) bool {
			if event.Kind != ui.InputKeyDown {
				return false
			}
			delta, pageTarget := 0, -1
			switch event.Key {
			case ui.KeyUp:
				delta = -1
			case ui.KeyDown:
				delta = 1
			case ui.KeyPageUp:
				// Move to the visible edge first, then advance by a visible page.
				first, last := p.tableState.Visible()
				pageSize := max(last-first, 1)
				if last >= first && p.selected > first && p.selected <= last {
					pageTarget = first
				} else {
					delta = -pageSize
				}
			case ui.KeyPageDown:
				first, last := p.tableState.Visible()
				pageSize := max(last-first, 1)
				if last >= first && p.selected >= first && p.selected < last {
					pageTarget = last
				} else {
					delta = pageSize
				}
			default:
				return false
			}
			if len(matches) > 0 {
				if pageTarget >= 0 {
					p.selected = pageTarget
				} else {
					p.selected += delta
				}
				if p.selected < 0 {
					p.selected = 0
				} else if p.selected >= len(matches) {
					p.selected = len(matches) - 1
				}
				p.tableState.ScrollIntoView(p.selected)
			}
			return true
		})
		if filter.Submitted() && !p.expired {
			p.choose(matches)
		}
	})
}

func (p *guiPickerState) matches() []identityMatch {
	if !p.optionsValid {
		p.searchOptions = makeSearchableIdentityOptions(p.identities)
		p.optionsValid = true
	}
	if p.matchesValid && p.matchesQuery == p.query {
		return p.matchesCache
	}
	p.matchesQuery = p.query
	p.matchesCache = matchIdentities(p.searchOptions, p.query)
	p.tableRows = make([]guitable.IdentityRow, len(p.matchesCache))
	for i, match := range p.matchesCache {
		p.tableRows[i] = guitable.IdentityRow{Identity: match.identity, Number: match.index + 1}
	}
	p.matchesValid = true
	return p.matchesCache
}

func (p *guiPickerState) choose(matches []identityMatch) {
	if p.expired || p.ctx.Err() != nil || p.selected < 0 || p.selected >= len(matches) {
		return
	}
	p.chosen = matches[p.selected].identity
	p.hasSelection = true
	p.focusTime = capturePickerRestoreTimestamp()
	p.closeWindow()
}

// closeWindow defers native destruction until MyGo finishes building this UI pass.
func (p *guiPickerState) closeWindow() {
	if p.closePending || p.window == nil {
		return
	}
	p.closePending = true
	window := p.window
	window.Update(func() { window.Close() })
}

func (p *guiPickerState) result() guiSelectionResult {
	if !p.hasSelection {
		return guiSelectionResult{err: ErrCancelled}
	}
	return guiSelectionResult{identity: p.chosen, focusTime: p.focusTime}
}

func (p *guiPickerState) refreshKeys() {
	if p.refresh == nil || p.refreshing || p.expired || p.ctx.Err() != nil {
		return
	}
	p.refreshing = true
	p.err = ""
	p.window.Invalidate()
	go func() {
		refreshCtx, cancel := context.WithTimeout(p.ctx, upstream.RequestTimeout)
		defer cancel()
		updated, err := p.refresh(refreshCtx)
		if p.window == nil || p.window.IsDestroyed() {
			return
		}
		p.window.Update(func() {
			p.refreshing = false
			if err != nil {
				p.err = err.Error()
				return
			}
			p.identities = guiAvailableIdentityMetadata(p.offered, updated)
			p.searchOptions = nil
			p.optionsValid = false
			p.matchesCache = nil
			p.matchesQuery = ""
			p.matchesValid = false
			p.tableRows = nil
			p.selected = 0
			if len(p.identities) == 0 {
				p.selected = -1
			}
		})
	}()
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

// guiAvailableIdentityMetadata refreshes display data without selecting keys the SSH client did not offer.
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
