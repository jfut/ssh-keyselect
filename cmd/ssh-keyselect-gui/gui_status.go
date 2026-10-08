//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	guiassets "github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/credits"
	"github.com/jfut/ssh-keyselect/internal/guitable"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

// These small vector icons keep the compact status layout crisp at any display scale.
var (
	guiRefreshIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 11a8.1 8.1 0 0 0-15.5-2M4 4v5h5m-5 4a8.1 8.1 0 0 0 15.5 2M20 20v-5h-5"/></svg>`))
	guiSettingsIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm0-5v2m0 14v2m9-9h-2M5 12H3m15.36-6.36-1.42 1.42M7.06 16.94l-1.42 1.42m12.72 0-1.42-1.42M7.06 7.06 5.64 5.64"/></svg>`))
	guiCopyIcon     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M8 8V5.5A1.5 1.5 0 0 1 9.5 4h9A1.5 1.5 0 0 1 20 5.5v11a1.5 1.5 0 0 1-1.5 1.5H16M5.5 8h9A1.5 1.5 0 0 1 16 9.5v10A1.5 1.5 0 0 1 14.5 21h-9A1.5 1.5 0 0 1 4 19.5v-10A1.5 1.5 0 0 1 5.5 8Z"/></svg>`))
	guiCloseIcon    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2" d="m6 6 12 12M18 6 6 18"/></svg>`))
)

// guiMainWindowTitle keeps the application name, endpoint, and unsaved state together.
func guiMainWindowTitle(endpoint string, dirty bool) string {
	title := branding.EndpointTitle(endpoint)
	if dirty {
		title += " *"
	}
	return title
}

func (a *guiApp) start() {
	a.uiStarted = true
	if runtime.GOOS != "darwin" {
		// macOS takes the Dock icon from the app bundle's ICNS resource.
		// Avoid replacing it with the source PNG, which drops the Finder icon treatment.
		icon, iconErr := guiassets.PNG(256)
		if iconErr != nil {
			a.logger.Warn("create application icon", "error", iconErr)
		} else if err := mygo.App.Dock.SetIcon(icon); err != nil {
			a.logger.Warn("set application icon", "error", err)
		}
	}

	a.createMainWindow()
	trayIcon, err := guiassets.WindowIconPNG()
	if err == nil {
		a.tray, err = mygo.NewTray(mygo.TrayOptions{
			Icon: trayIcon, ToolTip: branding.EndpointTitle(guiDisplayEndpointPath(a.actualListen)),
			Menu: a.trayMenu(),
		})
		if err == nil {
			// A left click also restores a window hidden by the minimize-to-tray behavior.
			a.tray.OnClick(a.showMainWindow)
		}
	}
	if err != nil {
		a.statusMessage = "System tray icon unavailable: " + err.Error()
		a.window.Invalidate()
		a.logger.Warn("create system tray icon", "error", err)
	}

	a.refreshIdentities()
	a.applyConfig(a.cfg, false, func(applyErr error) {
		if applyErr != nil {
			a.statusMessage = "Agent proxy could not start: " + applyErr.Error()
			a.logger.Error("listen on agent endpoint", "error", applyErr)
			if a.window != nil {
				a.window.Invalidate()
			}
		}
	})
}

func (a *guiApp) createMainWindow() {
	// Windows dimensions are the default; adjust other platforms to show five key
	// rows.
	width, height, minHeight := 920, 415, 385
	if runtime.GOOS == "darwin" {
		height, minHeight = 362, 332
	} else if runtime.GOOS == "linux" {
		height, minHeight = 352, 322
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     guiMainWindowTitle(guiDisplayEndpointPath(a.actualListen), a.dirty),
		Width:     width,
		Height:    height,
		MinWidth:  850,
		MinHeight: minHeight,
		Content:   ui.View(a.view),
	})
	a.window = window
	a.mainWindow.Store(window)
	a.mainVisible.Store(true)
	if icon, err := guiassets.WindowIconPNG(); err == nil {
		if err := window.SetIcon(icon); err != nil {
			a.logger.Warn("set window icon", "error", err)
		}
	}
	window.OnClose(func(event *mygo.CloseEvent) {
		if a.applying {
			event.PreventDefault()
			a.statusMessage = "Wait for the configuration change to finish before closing."
			window.Invalidate()
			return
		}
		if a.dirty && !a.closeConfirmed {
			event.PreventDefault()
			a.closePrompt = true
			window.Invalidate()
		}
	})
	window.OnMinimize(func() {
		if a.tray != nil {
			window.Hide()
			if runtime.GOOS == "darwin" {
				// Keep the menu bar icon available while the hidden window has no Dock entry.
				mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
			}
		}
	})
	window.OnShow(func() { a.mainVisible.Store(true) })
	window.OnHide(func() { a.mainVisible.Store(false) })
	window.OnClosed(func() {
		a.mainVisible.Store(false)
		if a.window == window {
			a.window = nil
		}
	})
}

func (a *guiApp) view(c *ui.Context) {
	theme := guitable.CompactTheme(c)
	content := ui.Column(c).Fill().Padding(theme.Space(3)).Gap(theme.Space(2))
	if runtime.GOOS != "darwin" {
		// App-level commands stay available regardless of which area has focus.
		if c.Shortcut(ui.Cmd, ui.KeyO) {
			a.openConfiguration()
		}
		if c.Shortcut(ui.Cmd, ui.KeyS) {
			a.saveConfigurationFromMenu()
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
			a.saveAsConfiguration()
		}
		if c.Shortcut(ui.Cmd, ui.KeyComma) {
			a.openSettings()
		}
		if c.Shortcut(ui.Cmd, ui.KeyQ) {
			mygo.App.Quit()
		}
	}
	content.Children(func() {
		if runtime.GOOS != "darwin" {
			a.applicationMenuBar(c, theme)
		}
		a.identityCard(c, theme)
		a.connectionCard(c, theme)
	})

	if a.confirmAutoSelect {
		ui.Modal(c, &a.confirmAutoSelect, func() {
			ui.Column(c).Width(440).Padding(theme.Space(4), theme.Space(4), 0, theme.Space(4)).Gap(theme.Space(3)).
				Background(theme.Background).Children(func() {
				ui.Text(c, "Enable Auto Select?").Bold().FontSize(theme.FontSize + 1)
				ui.Text(c, "Every upstream identity will be available to clients using this proxy. Any client that can access the proxy can request signatures with any loaded key. Use this only for trusted batch work, then turn it off.").TextColor(theme.TextMuted)
				guiDialogActionRow(c, theme, ui.End, func() {
					if guiDialogActionButton(c, "Cancel", false, false).Clicked() {
						a.confirmAutoSelect = false
					}
					enable := guiDialogActionButton(c, "Enable", true, false).AutoFocus()
					if enable.Clicked() {
						a.confirmAutoSelect = false
						a.autoSelect = true
						a.server.SetAutoSelect(true)
						a.updateTrayAutoSelect()
					}
				}).Margin(theme.Space(2), 0, 0, 0)
			})
		})
	}
	if a.closePrompt {
		switch ui.AlertDialog(c, &a.closePrompt, "Unsaved configuration",
			"Save the current configuration before closing?", "Cancel", "Discard", "Save") {
		case 1:
			a.closeConfirmed = true
			a.window.Close()
		case 2:
			if err := a.saveCurrentConfig(); err != nil {
				a.statusMessage = "Could not save configuration: " + err.Error()
				a.closePrompt = true
			} else {
				a.closeConfirmed = true
				a.window.Close()
			}
		}
	}
}

// applicationMenuBar draws a readable menu strip on platforms with native menus
// whose text size cannot be controlled by the MyGo public API.
func (a *guiApp) applicationMenuBar(c *ui.Context, theme *ui.Theme) {
	ui.Row(c).Gap(theme.Space(1)).AlignItems(ui.Center).
		Margin(-theme.Space(3), -theme.Space(3), 0, -theme.Space(3)).
		Padding(0, 0, theme.Space(1), 0).Background(ui.Hex("#ffffff")).
		BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		file := ui.Text(c, "File").FontSize(theme.Rem(0.9)).Padding(theme.Space(1), theme.Space(2)).Cursor(ui.CursorPointer)
		if file.Hovered() {
			file.Background(theme.SurfaceHover).Radius(theme.Space(1))
		}
		file.Menu(func(menu *ui.Menu) {
			if menu.Item("Open Configuration…").Shortcut(ui.Cmd, ui.KeyO).Disabled(a.applying).Chosen() {
				a.openConfiguration()
			}
			if menu.Item("Save Configuration").Shortcut(ui.Cmd, ui.KeyS).Disabled(a.applying).Chosen() {
				a.saveConfigurationFromMenu()
			}
			if menu.Item("Save Configuration As…").Shortcut(ui.Cmd|ui.Shift, ui.KeyS).Disabled(a.applying).Chosen() {
				a.saveAsConfiguration()
			}
			menu.Separator()
			if menu.Item("Settings…").Shortcut(ui.Cmd, ui.KeyComma).Disabled(a.applying).Chosen() {
				a.openSettings()
			}
			menu.Separator()
			if menu.Item("Quit").Shortcut(ui.Cmd, ui.KeyQ).Chosen() {
				mygo.App.Quit()
			}
		})
		help := ui.Text(c, "Help").FontSize(theme.Rem(0.9)).Padding(theme.Space(1), theme.Space(2)).Cursor(ui.CursorPointer)
		if help.Hovered() {
			help.Background(theme.SurfaceHover).Radius(theme.Space(1))
		}
		help.Menu(func(menu *ui.Menu) {
			if menu.Item("About").Chosen() {
				a.showAbout()
			}
		})
	})
}

func (a *guiApp) identityCard(c *ui.Context, theme *ui.Theme) {
	card := guiCard(c)
	// Keep selected row highlights inside the rounded key card on every OS.
	card.Clip()
	card.Grow(1).MinHeight(theme.Space(50)).Children(func() {
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Keys").TextColor(ui.Hex("#1870de")).
				Background(ui.Hex("#e8f2ff")).Padding(theme.Space(1), theme.Space(2)).Radius(theme.Space(1.5))
			switch {
			case a.loading:
				ui.Text(c, "Loading…").TextColor(theme.TextMuted).Grow(1)
			case a.keyError != "":
				ui.Text(c, "Could not load keys").TextColor(theme.Danger).Tooltip(a.keyError).Grow(1)
			default:
				ui.Text(c, a.keyStatus).TextColor(theme.TextMuted).Grow(1)
			}
			refresh := guiIconButton(c, guiRefreshIcon, "Refresh keys", true, theme)
			refresh.Size(theme.Space(8), theme.Space(8))
			refresh.Disabled(a.loading)
			if refresh.Clicked() {
				a.refreshIdentities()
			}
		})
		rows := a.sortedIdentities()
		a.identityTable.Selected = &a.selectedIdentity
		a.identityTable.Sort = &a.identitySort
		table := guitable.IdentityTable(c, &a.identityTable, rows, true, true).
			Grow(1).MinHeight(theme.Space(50))
		table.ContextMenu(func(menu *ui.Menu) {
			copyItem := menu.Item("Copy").Disabled(a.selectedIdentity < 0 || a.selectedIdentity >= len(rows)).
				Shortcut(ui.Cmd, ui.KeyC)
			if copyItem.Chosen() && a.selectedIdentity >= 0 && a.selectedIdentity < len(rows) {
				mygo.Clipboard.WriteText(guitable.IdentityCopyText(rows[a.selectedIdentity].Identity))
			}
		})
		if table.Shortcut(ui.Cmd, ui.KeyC) && a.selectedIdentity >= 0 && a.selectedIdentity < len(rows) {
			mygo.Clipboard.WriteText(guitable.IdentityCopyText(rows[a.selectedIdentity].Identity))
		}
		if len(rows) == 0 && !a.loading && a.keyError == "" {
			ui.Text(c, "No identities are available from the upstream agent.").TextColor(theme.TextMuted)
		}
	})
}

func (a *guiApp) connectionCard(c *ui.Context, theme *ui.Theme) {
	card := guiCard(c)
	card.Shrink(0).Children(func() {
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Agent Proxy").TextColor(ui.Hex("#16804a")).
				Background(ui.Hex("#e6f8ee")).Padding(theme.Space(1), theme.Space(2)).Radius(theme.Space(1.5))
			settings := guiIconButton(c, guiSettingsIcon, "Settings", true, theme)
			settings.Size(theme.Space(8), theme.Space(8))
			if settings.Clicked() && !a.applying {
				a.openSettings()
			}
			if a.statusMessage != "" {
				ui.Row(c).Grow(1).Gap(theme.Space(1)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.statusMessage).TextColor(theme.Warning).
						SingleLine().Tooltip(a.statusMessage).Shrink(1).MinWidth(0)
					dismiss := guiIconButton(c, guiCloseIcon, "Dismiss status message", false, theme)
					dismiss.Size(theme.Space(7), theme.Space(7))
					if dismiss.Clicked() {
						a.statusMessage = ""
					}
				})
			}
			autoSelect := a.server.AutoSelect()
			ui.Text(c, "Auto Select").TextColor(ui.Hex("#1870de")).
				Background(ui.Hex("#e8f2ff")).Padding(theme.Space(1), theme.Space(2)).Radius(theme.Space(1.5)).
				Margin(0, 0, 0, ui.Auto).
				Tooltip("When On, every upstream key is available without a per-connection selection.")
			off := ui.Button(c, "Off").Tooltip("Select one key for each connection.")
			if !autoSelect {
				off.Background(ui.Hex("#e6f8ee")).TextColor(ui.Hex("#16804a")).Border(1, ui.Hex("#e6f8ee"))
			}
			if off.Clicked() && autoSelect {
				a.autoSelect = false
				a.server.SetAutoSelect(false)
				a.updateTrayAutoSelect()
			}
			on := ui.Button(c, "On").Tooltip("Allow clients to use every upstream identity.")
			if autoSelect {
				on.Background(ui.Hex("#b62b2b")).TextColor(ui.Hex("#ffffff")).Border(1, ui.Hex("#b62b2b"))
			}
			if on.Clicked() && !autoSelect {
				a.confirmAutoSelect = true
			}
		})
		ui.Column(c).Gap(theme.Space(1)).Children(func() {
			a.endpointRow(c, theme, "Upstream", "UPSTREAM_SSH_AUTH_SOCK", false)
			a.endpointRow(c, theme, "Listen", "SSH_AUTH_SOCK", true)
		})
	})
}

func (a *guiApp) endpointRow(c *ui.Context, theme *ui.Theme, title, variable string, listen bool) {
	endpoint := ""
	mode := transport.Auto
	var modeErr error
	marker := ui.Hex("#1870de")
	modeFill, modeInk := ui.Hex("#e8f2ff"), ui.Hex("#1870de")
	if listen {
		endpoint, mode = a.actualListen, a.listenMode
		marker = ui.Hex("#16804a")
		modeFill, modeInk = ui.Hex("#e6f8ee"), ui.Hex("#16804a")
		if endpoint == "" {
			modeErr = fmt.Errorf("listen endpoint is not available")
		}
	} else if a.endpointAgent != nil {
		current, _ := a.endpointAgent.Snapshot()
		endpoint, mode = current.Path, current.Mode
		if endpoint != "" {
			mode, modeErr = upstream.ResolveMode(endpoint, mode)
		}
	}
	displayPath := guiDisplayEndpointPath(endpoint)
	if endpoint == "" {
		displayPath = "Not configured"
		modeFill, modeInk = ui.Hex("#fff6e0"), ui.Hex("#b56b00")
	} else if modeErr != nil {
		modeFill, modeInk = ui.Hex("#fff6e0"), ui.Hex("#b56b00")
	}
	controlHeight := theme.Space(8)
	ui.Row(c).Gap(theme.Space(1.5)).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(theme.Space(60)).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(theme.Space(0.75), theme.Space(11)).Radius(theme.Space(0.5)).Background(marker).Shrink(0)
			ui.Column(c).Gap(theme.Space(0.5)).Grow(1).Children(func() {
				ui.Text(c, title).FontSize(theme.Rem(0.95)).SingleLine()
				ui.Row(c).Gap(theme.Space(1)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, variable).FontSize(theme.Rem(0.82)).TextColor(theme.TextMuted).SingleLine()
					if listen {
						ui.Text(c, "Proxy").FontSize(theme.Rem(0.72)).TextColor(ui.Hex("#16804a")).
							Background(ui.Hex("#e6f8ee")).Padding(theme.Space(0.5), theme.Space(1)).Radius(theme.Space(1))
					}
				})
			})
		})
		path := ui.Text(c, displayPath).SingleLine().Selectable().Tooltip(displayPath).
			Padding(theme.Space(1), theme.Space(2)).Background(ui.Hex("#f7f9fc")).
			Border(1, theme.Border).Radius(theme.Space(1)).Grow(1).MinWidth(theme.Space(35)).Height(controlHeight)
		if endpoint == "" {
			path.TextColor(theme.TextMuted)
		}
		copyButton := guiIconButton(c, guiCopyIcon, "Copy "+variable+" export command", false, theme)
		copyButton.Size(theme.Space(8), controlHeight).Disabled(endpoint == "")
		if copyButton.Clicked() && endpoint != "" {
			pathToCopy := guiEndpointPathFromDisplay(displayPath)
			mygo.Clipboard.WriteText(guiEndpointExportCommand(variable, pathToCopy))
		}
		badge := ui.Row(c).Width(theme.Space(72)).Height(controlHeight).Padding(theme.Space(1.5), theme.Space(2)).
			Background(modeFill).Radius(theme.Space(1)).AlignItems(ui.Center)
		badge.Children(func() {
			label := "Not configured"
			if endpoint != "" && modeErr != nil {
				label = "Unavailable"
			} else if endpoint != "" {
				label = guiDisplayModeName(mode)
			}
			ui.Text(c, label).FontSize(theme.Rem(0.92)).TextColor(modeInk).SingleLine().Tooltip(modeErrString(modeErr))
		})
	})
}

func modeErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func guiIconButton(c *ui.Context, icon *ui.SVG, label string, primary bool, theme *ui.Theme) ui.Element {
	var button ui.Element
	if primary {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	button.Label(label).Tooltip(label).Padding(0).Children(func() {
		ui.Icon(c, icon).Size(theme.Space(4), theme.Space(4))
	})
	return button
}

func guiCard(c *ui.Context) ui.Element {
	theme := c.Theme()
	return ui.Column(c).Padding(theme.Space(2.5)).Gap(theme.Space(1.5)).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Space(3))
}

func (a *guiApp) sortedIdentities() []guitable.IdentityRow {
	if a.identityRowsValid && a.identityRowsSort == a.identitySort {
		return a.identityRowsCache
	}
	rows := make([]guitable.IdentityRow, len(a.identities))
	for i, id := range a.identities {
		rows[i] = guitable.IdentityRow{Identity: id, Number: i + 1}
	}
	if a.identitySort.Column == "" {
		a.identityRowsCache = rows
		a.identityRowsSort = a.identitySort
		a.identityRowsValid = true
		return rows
	}
	compare := func(left, right guitable.IdentityRow) int {
		if a.identitySort.Column == "number" {
			switch {
			case left.Number < right.Number:
				return -1
			case left.Number > right.Number:
				return 1
			default:
				return 0
			}
		}
		leftID, rightID := left.Identity, right.Identity
		var l, r string
		switch a.identitySort.Column {
		case "comment":
			l, r = identity.DisplayText(leftID.Comment), identity.DisplayText(rightID.Comment)
		case "type":
			l, r = identity.DisplayText(leftID.Algorithm), identity.DisplayText(rightID.Algorithm)
		case "size":
			l = identity.DisplayBitSize(leftID.Blob, leftID.Algorithm)
			r = identity.DisplayBitSize(rightID.Blob, rightID.Algorithm)
		case "fingerprint":
			l, r = identity.DisplayText(leftID.Fingerprint), identity.DisplayText(rightID.Fingerprint)
		}
		return strings.Compare(l, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		order := compare(rows[i], rows[j])
		if a.identitySort.Descending {
			return order > 0
		}
		return order < 0
	})
	a.identityRowsCache = rows
	a.identityRowsSort = a.identitySort
	a.identityRowsValid = true
	return rows
}

func (a *guiApp) refreshIdentities() {
	a.refreshGeneration++
	requestGeneration := a.refreshGeneration
	window := a.mainWindow.Load()
	endpoint, generation := a.endpointAgent.Snapshot()
	if endpoint.Path == "" {
		a.identities = nil
		a.identityRowsValid = false
		a.selectedIdentity = -1
		a.keyStatus = "Upstream not configured"
		a.keyError = ""
		a.loading = false
		if window != nil {
			window.Invalidate()
		}
		return
	}
	a.loading = true
	a.keyError = ""
	if window != nil {
		window.Invalidate()
	}
	go func() {
		requestCtx, cancel := context.WithTimeout(a.ctx, upstream.RequestTimeout)
		defer cancel()
		identities, err := endpoint.List(requestCtx)
		if window == nil {
			return
		}
		window.Update(func() {
			_, currentGeneration := a.endpointAgent.Snapshot()
			if currentGeneration != generation || requestGeneration != a.refreshGeneration {
				return
			}
			a.loading = false
			a.selectedIdentity = -1
			if err != nil {
				a.identities = nil
				a.keyError = err.Error()
				a.keyStatus = "Could not load keys"
			} else {
				a.identities = identities
				a.keyError = ""
				a.keyStatus = guiIdentityCount(len(identities))
			}
			a.identityRowsValid = false
			if a.window != nil {
				a.window.Invalidate()
			}
		})
	}()
}

func guiIdentityCount(count int) string {
	if count == 1 {
		return "1 key"
	}
	return fmt.Sprintf("%d keys", count)
}

func (a *guiApp) updateWindowTitle() {
	if a.window != nil && !a.window.IsDestroyed() {
		a.window.SetTitle(guiMainWindowTitle(guiDisplayEndpointPath(a.actualListen), a.dirty))
	}
	if a.tray != nil {
		a.tray.SetToolTip(branding.EndpointTitle(guiDisplayEndpointPath(a.actualListen)))
	}
}

func (a *guiApp) updateTrayAutoSelect() {
	if a.trayAutoSelect != nil {
		a.trayAutoSelect.SetChecked(a.server.AutoSelect())
	}
}

func (a *guiApp) showMainWindow() {
	if a.window == nil || a.window.IsDestroyed() {
		a.createMainWindow()
	}
	if runtime.GOOS == "darwin" {
		// Restore normal Dock and app switcher presence before showing the main window.
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	}
	// Hiding a minimized window may clear its minimized state on some backends.
	// Restore hidden windows too, then show them through MyGo's normal lifecycle.
	if a.window.IsMinimized() || !a.window.IsVisible() {
		a.window.Restore()
	}
	a.window.Show()
	a.mainVisible.Store(true)
	a.window.Focus()
}

func (a *guiApp) showAbout() {
	a.showMainWindow()
	if a.aboutWindow != nil && !a.aboutWindow.IsDestroyed() {
		a.aboutWindow.Show()
		a.aboutWindow.Focus()
		return
	}
	var aboutIcon *ui.Bitmap
	if data, err := guiassets.PNG(64); err == nil {
		aboutIcon, _ = ui.DecodeBitmap(data)
	}
	content := ui.View(func(c *ui.Context) {
		theme := guitable.CompactTheme(c)
		root := ui.Column(c).Fill().Padding(guiDialogEdgePadding(theme)).Gap(theme.Space(2)).Background(ui.Hex("#f5f8fc"))
		if c.Shortcut(0, ui.KeyEscape) && a.aboutWindow != nil {
			a.aboutWindow.Close()
			return
		}
		if root.Shortcut(ui.Cmd, ui.KeyC) {
			mygo.Clipboard.WriteText(guiAboutCopyText())
		}
		root.Children(func() {
			card := guiCard(c).Grow(1).Gap(theme.Space(1.5)).Padding(theme.Space(2))
			card.Children(func() {
				ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
					if aboutIcon != nil {
						ui.Image(c, aboutIcon).Size(32, 32).Shrink(0)
					}
					ui.Column(c).Gap(theme.Space(0.5)).Children(func() {
						ui.Text(c, branding.Name).FontWeight(500)
						ui.Text(c, branding.Subtitle).TextColor(theme.TextMuted)
					})
				})
				ui.Column(c).Gap(theme.Space(0.35)).Children(func() {
					ui.Textf(c, "Version: %s (%s)", version, commit).FontSize(theme.Rem(0.92)).Selectable()
					ui.Text(c, "Project URL: "+branding.ProjectURL).FontSize(theme.Rem(0.92)).Selectable()
					ui.Text(c, "Author: "+branding.Author).FontSize(theme.Rem(0.92)).Selectable()
				})
				ui.Text(c, "Third-party libraries and licenses").FontSize(theme.Rem(0.82)).
					Margin(theme.Space(1), 0, 0, 0)
				ui.Scroll(c).Grow(1).MinHeight(theme.Space(48)).Border(1, theme.Border).
					Radius(theme.Space(1)).Padding(theme.Space(1.5)).Background(ui.Hex("#ffffff")).Children(func() {
					ui.Text(c, strings.TrimSpace(credits.DependencyList)).FontSize(theme.Rem(0.9)).Selectable()
				})
				ui.Text(c, "Full license texts are included in CREDITS.").FontSize(theme.Rem(0.82))
			})
			guiDialogActionRow(c, theme, ui.End, func() {
				if guiDialogActionButton(c, "OK", true, false).Clicked() && a.aboutWindow != nil {
					a.aboutWindow.Close()
				}
			})
		})
	})
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "About " + branding.Name, Parent: a.window, Width: 500, Height: 510,
		MinWidth: 460, MinHeight: 460, Content: content,
	})
	a.aboutWindow = window
	if icon, err := guiassets.WindowIconPNG(); err == nil {
		_ = window.SetIcon(icon)
	}
	window.OnClosed(func() {
		if a.aboutWindow == window {
			a.aboutWindow = nil
		}
	})
}

// guiAboutCopyText formats the visible About details for the dialog's Ctrl+C shortcut.
func guiAboutCopyText() string {
	return strings.Join([]string{
		branding.Name,
		branding.Subtitle,
		fmt.Sprintf("Version: %s (%s)", version, commit),
		"Project URL: " + branding.ProjectURL,
		"Author: " + branding.Author,
		"Third-party libraries and licenses",
		strings.TrimSpace(credits.DependencyList),
		"Full license texts are included in CREDITS.",
	}, "\n")
}

// guiDisplayModeName is shared by status and settings surfaces.
func guiDisplayModeName(mode transport.Mode) string {
	switch mode {
	case transport.Auto:
		return "Auto Detection"
	case transport.Unix:
		return "Unix"
	case transport.Cygwin:
		return "Cygwin / Git for Windows"
	case transport.WSL1:
		return "WSL1"
	case transport.NamedPipe:
		return "Named Pipe (Windows OpenSSH)"
	default:
		return "unknown"
	}
}

func guiEndpointExportCommand(name, path string) string {
	return "export " + name + "='" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
}
