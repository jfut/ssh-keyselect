//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/accessibility"
	"github.com/richardwilkes/unison/enums/mod"
	"github.com/richardwilkes/unison/enums/role"
)

func TestGUIEndpointPathSelectionAndExportCopy(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "ssh keyselect", strings.Repeat("long-path-", 12), "agent.sock")
	displayPath := guiDisplayEndpointPath(endpoint)
	const upstreamEndpoint = `C:\Users\jun\.ssh\ssh-agent-keepass.sock`
	var window *unison.Window
	var update func(string, transport.Mode, error)
	screen, err := unison.StartHeadless(unison.HeadlessConfig{Width: 1200, Height: 600},
		unison.StartupFinishedCallback(func() {
			configureGUIAppearance()
			var createErr error
			window, createErr = unison.NewWindow("SSH KeySelect")
			if createErr != nil {
				t.Error(createErr)
				return
			}
			card := newGUIStatusCard()
			guiAddConnectionModeRow(card, guiAccentInk, guiKeysBadgeFill, guiKeysBadgeInk,
				"Upstream", "UPSTREAM_SSH_AUTH_SOCK", "", upstreamEndpoint, transport.Cygwin, nil)
			update = guiAddConnectionModeRow(card, guiActiveInk, guiActiveFillInk, guiActiveInk,
				"Listen", "SSH_AUTH_SOCK", "Proxy", endpoint, transport.Unix, nil)
			window.Content().SetLayout(&unison.FlexLayout{Columns: 1})
			window.Content().AddChild(card)
			window.Pack()
			window.Show()
			window.ToFront()
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Stop)
	if window == nil {
		t.Fatal("GUI window was not created")
	}

	var field *unison.Field
	var copyButton *unison.Button
	var upstreamCopyButton *unison.Button
	screen.Do(func() {
		var visit func(*unison.Panel)
		visit = func(panel *unison.Panel) {
			for _, child := range panel.Children() {
				switch widget := child.Self.(type) {
				case *unison.Field:
					if widget.Text() == displayPath {
						field = widget
					}
				case *unison.Button:
					switch widget.Accessibility.Name {
					case "Copy SSH_AUTH_SOCK export command":
						copyButton = widget
					case "Copy UPSTREAM_SSH_AUTH_SOCK export command":
						upstreamCopyButton = widget
					}
				}
				visit(child)
			}
		}
		visit(window.Content())
	})
	if field == nil || copyButton == nil || upstreamCopyButton == nil {
		t.Fatal("socket path field or endpoint copy button is missing")
	}

	// Drive a real mouse selection and shortcut so the display remains usable as plain text.
	var from, to geom.Point
	screen.Do(func() {
		inset := field.ContentRect(false).Point
		lineCenter := geom.NewPoint(0, field.Font.LineHeight()/2)
		from = field.FromSelectionIndex(5).Sub(inset).Add(lineCenter)
		to = field.FromSelectionIndex(18).Sub(inset).Add(lineCenter)
	})
	screen.Drag(screen.PanelPoint(field, from), screen.PanelPoint(field, to), 4)
	screen.KeyPress(unison.KeyC, mod.OSMenuCommand())
	selected := string([]rune(displayPath)[5:18])
	if got := unison.ClipboardGetText(); got != selected {
		t.Fatalf("mouse-selected clipboard = %q, want %q", got, selected)
	}
	screen.Type("x")
	screen.KeyPress(unison.KeyV, mod.OSMenuCommand())
	screen.KeyPress(unison.KeyX, mod.OSMenuCommand())
	screen.KeyPress(unison.KeyBackspace, mod.None)
	screen.KeyPress(unison.KeyDelete, mod.None)
	screen.Do(func() {
		if got := field.Text(); got != displayPath {
			t.Errorf("editing changed the socket path to %q", got)
		}
	})

	screen.ClickWith(screen.PanelPoint(field, from), unison.ButtonRight, mod.None)
	var copyItem accessibility.NodeID
	screen.AccessibilityTree(window).Walk(func(node *accessibility.Node) bool {
		if node.Role == role.MenuItem && node.Name == "Copy" {
			copyItem = node.ID
		}
		return true
	})
	if copyItem == 0 || !screen.PerformAccessibilityAction(accessibility.ActionRequest{Node: copyItem, Action: accessibility.Press}) {
		t.Fatal("could not copy the selected path from its context menu")
	}
	if got := unison.ClipboardGetText(); got != selected {
		t.Fatalf("context-menu clipboard = %q, want %q", got, selected)
	}
	screen.KeyPress(unison.KeyA, mod.OSMenuCommand())
	screen.KeyPress(unison.KeyC, mod.OSMenuCommand())
	if got := unison.ClipboardGetText(); got != displayPath {
		t.Fatalf("full path clipboard = %q, want the unabridged path %q", got, displayPath)
	}

	screen.Click(screen.PanelCenter(copyButton))
	if got, want := unison.ClipboardGetText(), `export SSH_AUTH_SOCK='`+endpoint+`'`; got != want {
		t.Fatalf("copy icon clipboard = %q, want %q", got, want)
	}
	screen.Click(screen.PanelCenter(upstreamCopyButton))
	if got, want := unison.ClipboardGetText(), `export UPSTREAM_SSH_AUTH_SOCK='C:\Users\jun\.ssh\ssh-agent-keepass.sock'`; got != want {
		t.Fatalf("upstream copy icon clipboard = %q, want %q", got, want)
	}
	// Windows must copy its native displayed path even when the listener uses a shell-form endpoint internally.
	const shellEndpoint = "/c/Users/jun/.ssh/ssh-keyselect-agent.sock"
	wantExport := `export SSH_AUTH_SOCK='/c/Users/jun/.ssh/ssh-keyselect-agent.sock'`
	if runtime.GOOS == "windows" {
		wantExport = `export SSH_AUTH_SOCK='C:\Users\jun\.ssh\ssh-keyselect-agent.sock'`
	}
	screen.Do(func() { update(shellEndpoint, transport.Cygwin, nil) })
	screen.Click(screen.PanelCenter(copyButton))
	if got := unison.ClipboardGetText(); got != wantExport {
		t.Fatalf("updated copy icon clipboard = %q, want %q", got, wantExport)
	}
	screen.Do(func() { update("", transport.Auto, nil) })
	screen.Click(screen.PanelCenter(copyButton))
	if got := unison.ClipboardGetText(); got != wantExport {
		t.Fatalf("unconfigured copy icon changed the clipboard to %q", got)
	}
	if errors := screen.Errors(); len(errors) > 0 {
		t.Fatalf("GUI reported headless errors: %v", errors)
	}
}
