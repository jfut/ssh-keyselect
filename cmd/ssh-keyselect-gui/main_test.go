//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/role"
)

func TestGUIListenCanBeResolvedWithoutAnUpstreamAgent(t *testing.T) {
	cfg := config.Default()
	path, mode, err := resolveGUIListen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || mode == "" {
		t.Fatalf("resolved listener = path %q, mode %q; both should be set", path, mode)
	}
}

func TestGUIInWindowMenuBarUsesWhiteBackground(t *testing.T) {
	root := unison.NewPanel()
	menuBar := unison.NewPanel()
	menuBar.Accessibility.Role = role.MenuBar
	menuScroll := unison.NewScrollPanel()
	menuBar.AddChild(menuScroll)
	root.AddChild(menuBar)

	styleGUIInWindowMenuBar(root)

	if got, ok := menuScroll.BackgroundInk.(unison.Color); !ok || got != guiMenuInk {
		t.Errorf("menu scroll background = %v, want white", menuScroll.BackgroundInk)
	}
}

func TestExecuteVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "ssh-keyselect-gui dev (none)\n" {
		t.Fatalf("version output = %q", got)
	}
}

func TestGUIMainWindowTitleKeepsInstanceNumberBeforeStatus(t *testing.T) {
	if got, want := guiMainWindowTitle(" (2)", true, " - tray icon unavailable"), "SSH KeySelect (2) * - tray icon unavailable"; got != want {
		t.Fatalf("GUI window title = %q, want %q", got, want)
	}
}
