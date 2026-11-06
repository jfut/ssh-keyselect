//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	guiassets "github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/guitable"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/selectionhistory"
)

const selectionHistoryPageSize = 50

// guiHistoryDatabasePath resolves the configured file or the per-user default.
func guiHistoryDatabasePath(userDataPath, configuredPath string) (string, error) {
	configuredPath = config.ExpandPath(strings.TrimSpace(configuredPath))
	if configuredPath == "" {
		configuredPath = filepath.Join(userDataPath, "history.sqlite")
	} else if !filepath.IsAbs(configuredPath) {
		configuredPath = filepath.Join(userDataPath, configuredPath)
	}
	path, err := filepath.Abs(configuredPath)
	if err != nil {
		return "", fmt.Errorf("resolve selection history database path: %w", err)
	}
	return path, nil
}

var selectionHistoryColumns = []ui.TableColumn{
	{ID: "requested", Title: "Requested", Width: 140, MinWidth: 125, Sortable: true},
	{ID: "target-host", Title: "Target host", Width: 145, MinWidth: 120, Sortable: true},
	{ID: "target-key", Title: "Target host key", Width: 185, MinWidth: 150, Sortable: true},
	{ID: "forwarded", Title: "Forwarded", Width: 205, MinWidth: 150, Sortable: true},
	{ID: "key-comment", Title: "Key comment", Width: 135, MinWidth: 110, Sortable: true},
	{ID: "key", Title: "Key", Width: 210, MinWidth: 160, Sortable: true},
	{ID: "key-fingerprint", Title: "Fingerprint", Width: 185, MinWidth: 155, Sortable: true},
	{ID: "selected", Title: "Selected at", Width: 140, MinWidth: 125, Sortable: true},
	{ID: "result", Title: "Result", Width: 82, MinWidth: 72, Sortable: true},
}

type guiHistoryState struct {
	store             *selectionhistory.Store
	window            *mygo.Window
	page              int
	total             int
	events            []selectionhistory.Event
	allEvents         []selectionhistory.Event
	sortedAllEvents   []selectionhistory.Event
	sortedAllOrder    ui.SortOrder
	allEventsLoaded   bool
	allEventsLoadFail bool
	observedSort      ui.SortOrder
	observedSortSet   bool
	sort              ui.SortOrder
	rememberSelection bool
	tableState        ui.ListState
	selectedRow       int
	loadGeneration    uint64
	loading           bool
	deleting          bool
	confirmDelete     bool
	pendingDelete     int64
	loadError         string
	status            string
}

// openHistory shows the persisted picker history in a separate native window.
func (a *guiApp) openHistory() {
	if !a.cfg.History.Enabled {
		return
	}
	if a.historyWindow != nil && !a.historyWindow.IsDestroyed() {
		a.historyWindow.Show()
		a.historyWindow.Focus()
		if a.historyView != nil && a.historyStore != nil {
			a.historyView.loadPage(a.historyView.page)
		}
		return
	}
	state := &guiHistoryState{
		store: a.historyStore, rememberSelection: a.cfg.History.RememberSelection, selectedRow: -1,
	}
	if a.historyError != "" {
		state.loadError = "Could not open history: " + a.historyError
	}
	// Fit all history columns at the default window width.
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "History - SSH KeySelect", Parent: a.window,
		Width: 1470, Height: 450, MinWidth: 820, MinHeight: 350,
		Content: ui.View(state.view),
	})
	state.window = window
	a.historyWindow = window
	a.historyView = state
	if icon, err := guiassets.WindowIconPNG(); err == nil {
		_ = window.SetIcon(icon)
	}
	window.OnClosed(func() {
		if a.historyWindow == window {
			a.historyWindow = nil
			a.historyView = nil
		}
	})
	window.Show()
	window.Focus()
	if state.store != nil {
		state.loadPage(0)
	}
}

// replaceHistoryStore activates a newly opened database without interrupting in-flight requests.
func (a *guiApp) replaceHistoryStore(store *selectionhistory.Store, path string) {
	var retired *selectionhistory.Store
	if a.historyStore != nil && a.historyStore != store {
		retired = a.historyStore
		a.historyStores = append(a.historyStores, a.historyStore)
	}
	a.historyStore = store
	a.historyPath = path
	a.historyError = ""
	if a.selector != nil {
		a.selector.SetHistoryOptions(store, a.cfg.History.Enabled, a.cfg.History.RememberSelection)
	}
	a.syncHistoryView()
	if retired != nil {
		go func() {
			if err := retired.Close(); err != nil && a.logger != nil {
				a.logger.Warn("close previous selection history database", "error", err)
			}
		}()
	}
}

func (a *guiApp) syncHistoryView() {
	if a.historyView != nil {
		state := a.historyView
		state.loadGeneration++
		state.loading = false
		state.page = 0
		state.total = 0
		state.events = nil
		state.clearAllEvents()
		state.selectedRow = -1
		state.loadError = ""
		state.rememberSelection = a.cfg.History.RememberSelection
		if !a.cfg.History.Enabled {
			state.store = nil
			if state.window != nil && !state.window.IsDestroyed() {
				state.window.Close()
			}
			return
		}
		state.store = a.historyStore
		if a.historyStore == nil && a.historyError != "" {
			state.loadError = "Could not open history: " + a.historyError
		}
		state.loadPage(0)
	}
}

func (s *guiHistoryState) clearAllEvents() {
	s.allEvents = nil
	s.sortedAllEvents = nil
	s.allEventsLoaded = false
	s.allEventsLoadFail = false
}

func (s *guiHistoryState) loadPage(page int) {
	if s.store == nil || s.window == nil || s.window.IsDestroyed() {
		return
	}
	if page < 0 {
		page = 0
	}
	s.selectedRow = -1
	if page != s.page {
		columns := s.tableState.Columns
		s.tableState = ui.ListState{Columns: columns}
		s.tableState.ScrollTo(0, ui.Start)
	}
	s.page = page
	s.loadError = ""
	s.status = ""
	if s.allEventsLoaded {
		s.loading = false
		s.window.Invalidate()
		return
	}
	if s.sort.Column != "" {
		s.allEventsLoadFail = false
		s.loadAllEvents()
		return
	}
	s.loading = true
	s.loadGeneration++
	generation := s.loadGeneration
	window, store := s.window, s.store
	release, err := store.Retain()
	if err != nil {
		s.loading = false
		s.loadError = "Could not load history: " + err.Error()
		window.Invalidate()
		return
	}
	window.Invalidate()
	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		events, total, err := store.ListPage(ctx, page, selectionHistoryPageSize)
		if window.IsDestroyed() {
			return
		}
		window.Update(func() {
			if window.IsDestroyed() || generation != s.loadGeneration {
				return
			}
			s.loading = false
			if err != nil {
				s.loadError = "Could not load history: " + err.Error()
				return
			}
			lastPage := max((total-1)/selectionHistoryPageSize, 0)
			if page > lastPage {
				s.loadPage(lastPage)
				return
			}
			s.events, s.total = events, total
		})
	}()
}

// loadAllEvents fetches one consistent snapshot when a header sort must span every page.
func (s *guiHistoryState) loadAllEvents() {
	if s.store == nil || s.window == nil || s.window.IsDestroyed() || s.loading {
		return
	}
	s.loading = true
	s.allEventsLoadFail = false
	s.loadError = ""
	s.status = ""
	s.selectedRow = -1
	s.loadGeneration++
	generation := s.loadGeneration
	window, store := s.window, s.store
	release, err := store.Retain()
	if err != nil {
		s.loading = false
		s.allEventsLoadFail = true
		s.loadError = "Could not load sorted history: " + err.Error()
		window.Invalidate()
		return
	}
	window.Invalidate()
	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		events, err := store.ListAll(ctx)
		if window.IsDestroyed() {
			return
		}
		window.Update(func() {
			if window.IsDestroyed() || generation != s.loadGeneration {
				return
			}
			s.loading = false
			if err != nil {
				s.allEventsLoadFail = true
				s.loadError = "Could not load sorted history: " + err.Error()
				return
			}
			s.allEvents = events
			s.sortedAllEvents = nil
			s.allEventsLoaded = true
			s.allEventsLoadFail = false
			s.events = nil
			s.total = len(events)
			lastPage := max((s.total-1)/selectionHistoryPageSize, 0)
			if s.page > lastPage {
				s.page = lastPage
			}
		})
	}()
}

func (s *guiHistoryState) refresh() {
	if s.store == nil {
		return
	}
	s.clearAllEvents()
	s.events = nil
	s.loadPage(s.page)
}

func (s *guiHistoryState) view(c *ui.Context) {
	theme := guitable.CompactTheme(c)
	root := ui.Column(c).Fill().Padding(theme.Space(3)).Gap(theme.Space(2))
	root.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space(2)).Children(func() {
			ui.Text(c, "History").FontWeight(500)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d requests", s.total)).TextColor(theme.TextMuted)
		})
		if s.loadError != "" {
			ui.Text(c, s.loadError).TextColor(theme.Danger)
		}
		if s.status != "" {
			ui.Text(c, s.status).TextColor(theme.Success)
		}
		card := ui.Column(c).Grow(1).MinHeight(theme.Space(42)).Padding(theme.Space(1.5)).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Space(2))
		card.Clip()
		card.Children(func() {
			events := s.pageEvents()
			switch {
			case s.loading && len(events) == 0:
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, "Loading history…").TextColor(theme.TextMuted)
				})
			case len(events) == 0:
				ui.Column(c).Grow(1).Center().Children(func() {
					if s.store == nil {
						ui.Text(c, "History is unavailable.").TextColor(theme.TextMuted)
					} else {
						ui.Text(c, "No history yet.").TextColor(theme.TextMuted)
					}
				})
			default:
				s.tableState.Key = func(row int) any { return events[row].ID }
				s.tableState.Selected = &s.selectedRow
				s.tableState.Sort = &s.sort
				table := ui.Table(c, &s.tableState, selectionHistoryColumns, len(events), func(row, column int) {
					event := events[row]
					cell := guitable.CompactCell(c, row, s.selectedRow, theme)
					cell.PopoverMenu(func(menu *ui.Menu) {
						copyRow := menu.Item("Copy row").Shortcut(ui.Cmd, ui.KeyC).Disabled(s.deleting)
						if copyRow.Chosen() && !s.deleting {
							mygo.Clipboard.WriteText(selectionHistoryCopyText(event))
						}
						menu.Separator()
						deleteRow := menu.Item("Delete row").Disabled(s.deleting)
						if deleteRow.Chosen() && !s.deleting {
							s.pendingDelete = event.ID
							s.confirmDelete = true
						}
					})
					cell.Children(func() {
						value := selectionHistoryCell(event, selectionHistoryColumns[column].ID)
						ui.Text(c, value).SingleLine().Tooltip(value)
					})
				}).Grow(1).FontSize(theme.Rem(1) - 1)
				if s.selectedRow >= 0 && s.selectedRow < len(events) && table.Shortcut(ui.Cmd, ui.KeyC) {
					mygo.Clipboard.WriteText(selectionHistoryCopyText(events[s.selectedRow]))
				}
				if s.sort.Column != "" && !s.allEventsLoaded && !s.loading && !s.allEventsLoadFail {
					s.loadAllEvents()
				}
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space(1.5)).Children(func() {
			previous := guiIconButton(c, guiChevronLeft, "Previous page", false, theme).
				Size(theme.Space(8), theme.Space(8)).
				Disabled(s.loading || s.deleting || s.page == 0 || s.store == nil)
			if previous.Clicked() {
				s.loadPage(s.page - 1)
			}
			totalPages := max((s.total+selectionHistoryPageSize-1)/selectionHistoryPageSize, 1)
			ui.Text(c, fmt.Sprintf("%d / %d", s.page+1, totalPages)).TextColor(theme.TextMuted)
			next := guiIconButton(c, guiChevronRight, "Next page", false, theme).
				Size(theme.Space(8), theme.Space(8)).
				Disabled(s.loading || s.deleting || s.page+1 >= totalPages || s.store == nil)
			if next.Clicked() {
				s.loadPage(s.page + 1)
			}
			refresh := guiIconButton(c, guiRefreshIcon, "Refresh history", true, theme)
			refresh.Size(theme.Space(8), theme.Space(8))
			refresh.Disabled(s.loading || s.deleting || s.store == nil)
			if refresh.Clicked() {
				s.refresh()
			}
			ui.Spacer(c)
			export := ui.PrimaryButton(c, "Export CSV").Disabled(s.loading || s.deleting || s.total == 0 || s.store == nil)
			if export.Clicked() {
				go s.exportCSV(s.store)
			}
		})
	})
	if s.confirmDelete {
		if ui.AlertDialog(c, &s.confirmDelete, "Delete history row?", "This removes the selected event from the history database.", "Cancel", "Delete") == 1 {
			s.deleteEvent(s.pendingDelete)
		}
	}
}

func selectionHistoryCopyText(event selectionhistory.Event) string {
	values := make([]string, 0, len(selectionHistoryColumns))
	for _, column := range selectionHistoryColumns {
		values = append(values, selectionHistoryCell(event, column.ID))
	}
	return strings.Join(values, "\t")
}

// pageEvents sorts all rows when needed, then returns only the requested display page.
func (s *guiHistoryState) pageEvents() []selectionhistory.Event {
	if !s.observedSortSet || s.observedSort != s.sort {
		s.selectedRow = -1
		s.observedSort = s.sort
		s.observedSortSet = true
		s.allEventsLoadFail = false
		s.loadError = ""
	}
	if s.allEventsLoaded {
		if s.sortedAllEvents == nil || s.sortedAllOrder != s.sort {
			s.sortedAllEvents = sortHistoryEvents(s.allEvents, s.sort)
			s.sortedAllOrder = s.sort
		}
		start := min(s.page*selectionHistoryPageSize, len(s.sortedAllEvents))
		end := min(start+selectionHistoryPageSize, len(s.sortedAllEvents))
		return s.sortedAllEvents[start:end]
	}
	return sortHistoryEvents(s.events, s.sort)
}

func sortHistoryEvents(source []selectionhistory.Event, order ui.SortOrder) []selectionhistory.Event {
	events := append([]selectionhistory.Event(nil), source...)
	if order.Column == "" {
		return events
	}
	sort.SliceStable(events, func(i, j int) bool {
		comparison := compareHistoryEvents(events[i], events[j], order.Column)
		if order.Descending {
			return comparison > 0
		}
		return comparison < 0
	})
	return events
}

func compareHistoryEvents(left, right selectionhistory.Event, column string) int {
	switch column {
	case "requested":
		return left.RequestedAt.Compare(right.RequestedAt)
	case "selected":
		return left.SelectedAt.Compare(right.SelectedAt)
	default:
		leftValue := strings.ToLower(selectionHistoryCell(left, column))
		rightValue := strings.ToLower(selectionHistoryCell(right, column))
		return strings.Compare(leftValue, rightValue)
	}
}

func (s *guiHistoryState) deleteEvent(eventID int64) {
	if s.store == nil || eventID < 1 || s.deleting {
		return
	}
	s.deleting = true
	s.loadError = ""
	if s.window != nil {
		s.window.Invalidate()
	}
	window, store := s.window, s.store
	rememberSelection := s.rememberSelection
	release, err := store.Retain()
	if err != nil {
		s.deleting = false
		s.loadError = "Could not delete history row: " + err.Error()
		if window != nil {
			window.Invalidate()
		}
		return
	}
	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var err error
		if rememberSelection {
			err = store.Delete(ctx, eventID)
		} else {
			err = store.DeleteHistory(ctx, eventID)
		}
		if window == nil || window.IsDestroyed() {
			return
		}
		window.Update(func() {
			if window.IsDestroyed() {
				return
			}
			s.deleting = false
			s.pendingDelete = 0
			if err != nil {
				s.loadError = "Could not delete history row: " + err.Error()
				return
			}
			s.clearAllEvents()
			s.events = nil
			s.loadPage(s.page)
		})
	}()
}

func selectionHistoryCell(event selectionhistory.Event, column string) string {
	switch column {
	case "requested":
		return formatHistoryTime(event.RequestedAt)
	case "target-host":
		return historyTargetHostName(event)
	case "target-key":
		return historyTargetHostKey(event)
	case "forwarded":
		return historyForwardedLabel(event.HostHops)
	case "key-comment":
		if event.SelectedKeyFingerprint == "" {
			return "-"
		}
		return displayHistoryValue(event.SelectedKeyComment)
	case "key":
		if event.SelectedKeyFingerprint == "" || len(event.SelectedKeyBlob) == 0 {
			return "-"
		}
		algorithm := displayHistoryValue(event.SelectedKeyAlgorithm)
		if algorithm == "-" {
			return base64.StdEncoding.EncodeToString(event.SelectedKeyBlob)
		}
		return algorithm + " " + base64.StdEncoding.EncodeToString(event.SelectedKeyBlob)
	case "key-fingerprint":
		if event.SelectedKeyFingerprint == "" {
			return "-"
		}
		return displayHistoryValue(event.SelectedKeyFingerprint)
	case "selected":
		if event.SelectedAt.IsZero() {
			return "-"
		}
		return formatHistoryTime(event.SelectedAt)
	case "result":
		switch event.Outcome {
		case selectionhistory.OutcomeSelected:
			return "Selected"
		case selectionhistory.OutcomeCancelled:
			return "Cancelled"
		case selectionhistory.OutcomeTimedOut:
			return "Timed out"
		case selectionhistory.OutcomeNoKeys:
			return "No keys"
		default:
			return "Failed"
		}
	default:
		return ""
	}
}

func formatHistoryTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04:05 MST")
}

func displayHistoryValue(value string) string {
	if safe := identity.DisplayText(value); safe != "" {
		return safe
	}
	return "-"
}

func historyTargetHostName(event selectionhistory.Event) string {
	if event.TargetHostFingerprint == "" {
		return "-"
	}
	if len(event.HostHops) == 0 {
		return "-"
	}
	target := event.HostHops[len(event.HostHops)-1]
	if target.IsForwarding {
		return "-"
	}
	names := make([]string, 0, len(target.KnownHosts))
	for _, name := range target.KnownHosts {
		if safe := identity.DisplayText(name); safe != "" {
			names = append(names, safe)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

func historyTargetHostKey(event selectionhistory.Event) string {
	if event.TargetHostFingerprint == "" {
		return "-"
	}
	key := displayHistoryValue(event.TargetHostFingerprint)
	for index := len(event.HostHops) - 1; index >= 0; index-- {
		hop := event.HostHops[index]
		if hop.IsForwarding || hop.Fingerprint != event.TargetHostFingerprint {
			continue
		}
		algorithm := displayHistoryValue(hop.Algorithm)
		if algorithm != "-" {
			return algorithm + " " + key
		}
		break
	}
	return key
}

func historyForwardedLabel(hops []selectionhistory.HostHop) string {
	var forwarded []string
	for _, hop := range hops {
		if !hop.IsForwarding {
			continue
		}
		key := displayHistoryValue(hop.Fingerprint)
		if algorithm := displayHistoryValue(hop.Algorithm); algorithm != "-" && key != "-" {
			key = algorithm + " " + key
		}
		label := key
		if len(hop.KnownHosts) > 0 {
			names := make([]string, 0, len(hop.KnownHosts))
			for _, name := range hop.KnownHosts {
				if safe := identity.DisplayText(name); safe != "" {
					names = append(names, safe)
				}
			}
			if len(names) > 0 {
				label = strings.Join(names, ", ")
				if key != "-" {
					label += " [" + key + "]"
				}
			}
		}
		if label == "" {
			label = "-"
		}
		forwarded = append(forwarded, label)
	}
	if len(forwarded) == 0 {
		return "-"
	}
	return strings.Join(forwarded, " -> ")
}

func (s *guiHistoryState) exportCSV(store *selectionhistory.Store) {
	if store == nil || s.window == nil || s.window.IsDestroyed() {
		return
	}
	window := s.window
	release, err := store.Retain()
	if err != nil {
		window.Update(func() {
			if !window.IsDestroyed() {
				s.loadError = "Could not export history: " + err.Error()
				window.Invalidate()
			}
		})
		return
	}
	defer release()
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: window, Title: "Export History", DefaultPath: guiDefaultHistoryCSVPath(),
		Filters: []mygo.FileFilter{{Name: "CSV files", Extensions: []string{"csv"}}},
	})
	if err == nil && path != "" {
		if filepath.Ext(path) == "" {
			path += ".csv"
		} else if !strings.EqualFold(filepath.Ext(path), ".csv") {
			err = fmt.Errorf("history export file must use the .csv extension")
		}
	}
	if err != nil {
		message := "Could not export history: " + err.Error()
		window.Update(func() {
			if !window.IsDestroyed() {
				s.loadError = message
				window.Invalidate()
			}
		})
		return
	}
	if path == "" {
		return
	}
	if err := writeGUIHistoryCSV(path, store); err != nil {
		message := "Could not export history: " + err.Error()
		window.Update(func() {
			if !window.IsDestroyed() {
				s.loadError = message
				window.Invalidate()
			}
		})
		return
	}
	window.Update(func() {
		if !window.IsDestroyed() {
			s.status = ""
			s.loadError = ""
			window.Invalidate()
		}
	})
}

func guiDefaultHistoryCSVPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return "history.csv"
	}
	return filepath.Join(base, "ssh-keyselect", "history.csv")
}

// writeGUIHistoryCSV keeps a failed export from replacing an existing destination.
func writeGUIHistoryCSV(path string, store *selectionhistory.Store) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".selection-history-*.csv")
	if err != nil {
		return fmt.Errorf("create CSV temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set CSV file permissions: %w", err)
	}
	if err := store.ExportCSV(context.Background(), temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync CSV file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close CSV file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace CSV file: %w", err)
	}
	return nil
}
