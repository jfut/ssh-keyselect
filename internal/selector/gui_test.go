//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/guiidentitytable"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/accessibility"
	"github.com/richardwilkes/unison/enums/mod"
	"github.com/richardwilkes/unison/enums/role"
)

func TestStoppedGUISelectorCancelsWithoutStartingTheUI(t *testing.T) {
	gui := NewGUISelector()
	gui.Stop()
	_, err := gui.Select(context.Background(), []identity.Identity{{Comment: "key"}})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Select error = %v, want cancellation", err)
	}
}

func TestGUISelectorCopiesKeyFromShortcutAndContextMenu(t *testing.T) {
	identities := []identity.Identity{guiTestIdentity(t, "first", 1), guiTestIdentity(t, "second", 2)}
	requestContext := SelectionContext{HostBindings: []HostBinding{
		{
			Algorithm:    "ssh-ed25519",
			Fingerprint:  "SHA256:forwarding-host",
			KnownHosts:   []string{"gateway.example.test"},
			IsForwarding: true,
		},
		{
			Algorithm:   "ssh-ed25519",
			Fingerprint: "SHA256:destination-host",
			KnownHosts:  []string{"node.example.test"},
		},
	}}
	shownAt := time.Date(2026, time.September, 27, 12, 34, 56, 789_000_000, time.FixedZone("JST", 9*60*60))
	var window *unison.Window
	screen, err := unison.StartHeadless(unison.HeadlessConfig{Width: 900, Height: 600},
		unison.StartupFinishedCallback(func() {
			var createErr error
			window, _, createErr = guiNewSelectionWindow(context.Background(), identities, nil, requestContext, shownAt)
			if createErr != nil {
				t.Error(createErr)
				return
			}
			window.Show()
			window.ToFront()
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Stop)

	var detailsField, filterField *unison.Field
	var detailsScroll *unison.ScrollPanel
	var title, watermark, details string
	screen.Do(func() {
		title = window.Title()
		for _, field := range guiFields(window.Content()) {
			if field.AllowsMultipleLines() {
				detailsField = field
			} else if strings.HasPrefix(field.Watermark, "> Filter (") {
				filterField = field
				watermark = field.Watermark
			}
		}
		if detailsField != nil {
			details = detailsField.Text()
			detailsScroll = guiScrollPanelForContent(window.Content(), detailsField)
		}
	})
	if got, want := title, "Select an SSH key to allow [SSH KeySelect - 2026-09-27 12:34:56 JST]"; got != want {
		t.Errorf("window title = %q, want %q", got, want)
	}
	if detailsField == nil || filterField == nil {
		t.Fatal("GUI request details text area or filter field is missing")
	}
	if want := "[SSH KeySelect - 2026-09-27 12:34:56 JST]\n├─ Forwarding hop 1"; !strings.HasPrefix(details, want) {
		t.Errorf("GUI request details should put the host context directly after the timestamp, got %q", details)
	}
	for _, expected := range []string{
		"├─ Forwarding hop 1\n│  Host key: ssh-ed25519 SHA256:forwarding-host\n│  known_hosts hints: gateway.example.test",
		"└─ Current target host\n   Host key: ssh-ed25519 SHA256:destination-host\n   known_hosts hints: node.example.test",
	} {
		if !strings.Contains(details, expected) {
			t.Errorf("GUI request details are missing %q:\n%s", expected, details)
		}
	}
	if detailsScroll == nil {
		t.Fatal("GUI request details scroll panel is missing")
	}
	screen.Do(func() {
		position := detailsScroll.Bar(false).Value()
		if maximum := detailsScroll.Bar(false).MaxValue(); position != maximum || maximum <= 0 {
			t.Errorf("request details scroll position = %v of %v, want the bottom", position, maximum)
		}
	})
	if strings.Contains(details, identitySelectionHint) {
		t.Error("cancel hint should be shown in the filter watermark, not in the details text area")
	}
	if got, want := watermark, "> Filter (If you don't recognize this request, press Esc to cancel.)"; got != want {
		t.Errorf("filter watermark = %q, want %q", got, want)
	}
	screen.Do(func() {
		detailsField.SelectAll()
		detailsField.Copy()
		if detailsField.RuneTypedCallback('x') != true {
			t.Error("details text area should consume typed characters without editing")
		}
		if got := detailsField.Text(); got != details {
			t.Errorf("typing changed read-only request details to %q", got)
		}
	})
	if got := unison.ClipboardGetText(); got != details {
		t.Errorf("copied request details = %q, want %q", got, details)
	}

	screen.KeyPress(unison.KeyC, mod.Control)
	if got, want := unison.ClipboardGetText(), "first\tssh-ed25519\t255\t"+identities[0].Fingerprint; got != want {
		t.Fatalf("shortcut clipboard = %q, want %q", got, want)
	}

	var scroller *unison.ScrollPanel
	screen.Do(func() { scroller = guiIdentityTableScrollPanel(window.Content()) })
	if scroller == nil {
		t.Fatal("GUI identity table scroll panel is missing")
	}
	rightClick := screen.PanelPoint(scroller, geom.NewPoint(20, guiidentitytable.HeaderHeight+guiidentitytable.RowHeight*1.5))
	screen.ClickWith(rightClick, unison.ButtonRight, mod.None)
	tree := screen.AccessibilityTree(window)
	var copyItem accessibility.NodeID
	tree.Walk(func(node *accessibility.Node) bool {
		if node.Role == role.MenuItem && node.Name == "Copy" {
			copyItem = node.ID
			return false
		}
		return true
	})
	if copyItem == 0 {
		t.Fatal("right-click menu has no Copy item")
	}
	if !screen.PerformAccessibilityAction(accessibility.ActionRequest{Node: copyItem, Action: accessibility.Press}) {
		t.Fatal("could not activate the context-menu Copy item")
	}
	if got, want := unison.ClipboardGetText(), "second\tssh-ed25519\t255\t"+identities[1].Fingerprint; got != want {
		t.Fatalf("context-menu clipboard = %q, want %q", got, want)
	}
	if errors := screen.Errors(); len(errors) > 0 {
		t.Fatalf("GUI selector reported headless errors: %v", errors)
	}
}

func guiFields(panel *unison.Panel) []*unison.Field {
	var fields []*unison.Field
	for _, child := range panel.Children() {
		if field, ok := child.Self.(*unison.Field); ok {
			fields = append(fields, field)
		}
		fields = append(fields, guiFields(child)...)
	}
	return fields
}

func guiScrollPanelForContent(panel *unison.Panel, content unison.Paneler) *unison.ScrollPanel {
	for _, child := range panel.Children() {
		if scroll, ok := child.Self.(*unison.ScrollPanel); ok {
			if scrolled := scroll.Content(); scrolled != nil && scrolled.AsPanel() == content.AsPanel() {
				return scroll
			}
		}
		if scroll := guiScrollPanelForContent(child, content); scroll != nil {
			return scroll
		}
	}
	return nil
}

func guiIdentityTableScrollPanel(panel *unison.Panel) *unison.ScrollPanel {
	for _, child := range panel.Children() {
		if scroll, ok := child.Self.(*unison.ScrollPanel); ok {
			if _, ok := scroll.Content().(*unison.Table[*guiidentitytable.Row]); ok {
				return scroll
			}
		}
		if scroll := guiIdentityTableScrollPanel(child); scroll != nil {
			return scroll
		}
	}
	return nil
}

func guiTestIdentity(t *testing.T, comment string, suffix byte) identity.Identity {
	t.Helper()
	blob, err := hex.DecodeString("0000000b7373682d65643235353139000000200000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] = suffix
	id, err := identity.New(blob, []byte(comment))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
