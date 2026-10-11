//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/guitable"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

// guiConfigForDisk stores endpoint paths in the native format for this OS.
func guiConfigForDisk(cfg config.Config) config.Config {
	cfg.Agent.Listen = guiListenPathForConfig(cfg.Agent.Listen)
	return cfg
}

func (a *guiApp) applyConfig(cfg config.Config, dirty bool, complete func(error)) {
	if a.applying {
		if complete != nil {
			complete(errors.New("configuration is already being applied"))
		}
		return
	}
	a.applying = true
	a.statusMessage = ""
	a.updateConfigurationMenu()
	window := a.mainWindow.Load()
	if window != nil {
		window.Invalidate()
	}
	go func() {
		actualListen, effectiveMode, err := a.runtime.Apply(cfg)
		if window == nil {
			return
		}
		window.Update(func() {
			a.applying = false
			a.updateConfigurationMenu()
			if a.ctx.Err() != nil {
				return
			}
			if err == nil {
				cfg.Agent.Upstream = config.ExpandPath(cfg.Agent.Upstream)
				cfg.Agent.Listen = config.ExpandPath(cfg.Agent.Listen)
				cfg.Log.File = config.ExpandPath(cfg.Log.File)
				a.cfg = cfg
				mygo.Theme.SetSource(mygo.ThemeSource(cfg.GUI.Theme))
				a.actualListen = actualListen
				a.listenMode = effectiveMode
				a.dirty = dirty
				a.statusMessage = ""
				if actualListen == "" {
					a.statusMessage = "Listen endpoint is unavailable or already in use."
				}
				a.updateWindowTitle()
				a.refreshIdentities()
			} else {
				a.statusMessage = "Could not apply configuration: " + err.Error()
			}
			if complete != nil {
				complete(err)
			}
			if a.window != nil {
				a.window.Invalidate()
			}
		})
	}()
}

func (a *guiApp) saveCurrentConfig() error {
	if err := config.Save(a.configPath, guiConfigForDisk(a.cfg)); err != nil {
		return err
	}
	a.dirty = false
	a.updateWindowTitle()
	return nil
}

// saveCurrentConfigWithCompletion saves an untitled configuration through Save As.
func (a *guiApp) saveCurrentConfigWithCompletion(complete func(bool, error)) {
	if a.configPath == "" {
		a.saveAsConfigurationWithCompletion(complete)
		return
	}
	err := a.saveCurrentConfig()
	if complete != nil {
		complete(err == nil, err)
	}
}

// newConfiguration protects unsaved edits before opening a fresh untitled configuration.
func (a *guiApp) newConfiguration() {
	if a.applying {
		return
	}
	a.showMainWindow()
	parent, currentPath, dirty := a.window, a.configPath, a.dirty
	if !dirty {
		a.applyNewConfiguration()
		return
	}
	go func() {
		detail := ""
		if currentPath != "" {
			detail = guiDisplayEndpointPath(currentPath)
		}
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: parent, Type: mygo.MessageQuestion, Title: branding.Name,
			Message: "Save changes before creating a new configuration?",
			Detail:  detail,
			Buttons: []string{"Cancel", "Discard", "Save"}, CancelButton: 0, DefaultButton: 2,
		})
		if err != nil || result.Button == 0 || parent == nil || parent.IsDestroyed() {
			return
		}
		if result.Button == 2 {
			parent.Update(func() {
				a.saveCurrentConfigWithCompletion(func(saved bool, saveErr error) {
					if saveErr != nil {
						a.statusMessage = "Could not save configuration: " + saveErr.Error()
						parent.Invalidate()
						return
					}
					if saved {
						a.applyNewConfiguration()
					}
				})
			})
			return
		}
		parent.Update(func() { a.applyNewConfiguration() })
	}()
}

// applyNewConfiguration resets settings and opens Settings after runtime apply succeeds.
func (a *guiApp) applyNewConfiguration() {
	if a.applying {
		return
	}
	cfg, err := guiDefaultNewConfiguration()
	if err != nil {
		a.statusMessage = "Could not create default upstream endpoint: " + err.Error()
		if a.window != nil {
			a.window.Invalidate()
		}
		return
	}
	a.applyConfig(cfg, false, func(err error) {
		if err != nil {
			return
		}
		a.configPath = ""
		a.confirmAutoSelect = false
		a.autoSelect = false
		a.server.SetAutoSelect(false)
		a.updateTrayAutoSelect()
		a.openSettings()
	})
}

// guiDefaultNewConfiguration starts fresh while retaining environment endpoints.
func guiDefaultNewConfiguration() (config.Config, error) {
	cfg := config.Default()
	cfg.Agent.Upstream = config.UpstreamFromEnvironment()
	var err error
	cfg, err = guiDefaultUpstreamIfUnset(cfg)
	if err != nil {
		return config.Config{}, err
	}
	if listen := os.Getenv("SSH_KEYSELECT_LISTEN"); listen != "" {
		cfg.Agent.Listen = config.ExpandPath(listen)
	}
	return cfg, nil
}

// guiDefaultUpstreamIfUnset provides a platform agent endpoint only when no upstream is configured.
func guiDefaultUpstreamIfUnset(cfg config.Config) (config.Config, error) {
	if cfg.Agent.Upstream == "" {
		endpoint, err := guiFallbackUpstreamEndpoint(cfg.Agent.UpstreamMode)
		if err != nil {
			return config.Config{}, fmt.Errorf("locate default upstream endpoint: %w", err)
		}
		cfg.Agent.Upstream = endpoint
	}
	cfg.Agent.Upstream = config.ExpandPath(cfg.Agent.Upstream)
	return cfg, nil
}

func (a *guiApp) openConfiguration() {
	if a.applying {
		return
	}
	a.showMainWindow()
	parent, currentPath, currentCfg, dirty := a.window, a.configPath, a.cfg, a.dirty
	go func() {
		path, err := guiChooseConfigToOpen(parent, currentPath)
		if err != nil {
			mygo.Dialog.Error("Could not open configuration.", err.Error())
			return
		}
		if path == "" {
			return
		}
		saved := false
		if dirty {
			result, err := mygo.Dialog.Message(mygo.MessageOptions{
				Parent: parent, Type: mygo.MessageQuestion, Title: branding.Name,
				Message: "Save changes before opening another configuration?",
				Detail:  guiDisplayEndpointPath(currentPath),
				Buttons: []string{"Cancel", "Discard", "Save"}, CancelButton: 0, DefaultButton: 2,
			})
			if err != nil || result.Button == 0 {
				return
			}
			if result.Button == 2 {
				savePath := currentPath
				if savePath == "" {
					savePath, err = guiChooseConfigToSave(parent, "")
					if err != nil {
						mygo.Dialog.Error("Could not save configuration.", err.Error())
						return
					}
					if savePath == "" {
						return
					}
				}
				if err := config.Save(savePath, guiConfigForDisk(currentCfg)); err != nil {
					mygo.Dialog.Error("Could not save configuration.", err.Error())
					return
				}
				currentPath = savePath
				saved = true
			}
		}
		cfg, err := config.Load(path)
		if err != nil {
			mygo.Dialog.Error("Could not open configuration.", err.Error())
			if saved && parent != nil {
				parent.Update(func() {
					a.configPath = currentPath
					a.dirty = false
					a.updateWindowTitle()
				})
			}
			return
		}
		if parent == nil || parent.IsDestroyed() {
			return
		}
		parent.Update(func() {
			if saved {
				a.configPath = currentPath
				a.dirty = false
			}
			a.applyConfig(cfg, false, func(applyErr error) {
				if applyErr == nil {
					a.configPath = path
					a.dirty = false
					a.updateWindowTitle()
				}
			})
		})
	}()
}

func (a *guiApp) saveAsConfiguration() {
	a.saveAsConfigurationWithCompletion(nil)
}

// saveAsConfigurationWithCompletion reports whether the native save dialog wrote a file.
func (a *guiApp) saveAsConfigurationWithCompletion(complete func(bool, error)) {
	if a.applying {
		return
	}
	a.showMainWindow()
	parent, currentPath, cfg := a.window, a.configPath, a.cfg
	go func() {
		path, err := guiChooseConfigToSave(parent, currentPath)
		if err == nil && path != "" {
			err = config.Save(path, guiConfigForDisk(cfg))
		}
		if err != nil {
			mygo.Dialog.Error("Could not save configuration.", err.Error())
		}
		if parent == nil || parent.IsDestroyed() {
			return
		}
		parent.Update(func() {
			if err == nil && path != "" {
				a.configPath = path
				a.dirty = false
				a.statusMessage = ""
				a.updateWindowTitle()
				if complete != nil {
					complete(true, nil)
				}
				return
			}
			if complete != nil {
				complete(false, err)
			}
		})
	}()
}

func (a *guiApp) saveConfigurationFromMenu() {
	if a.applying {
		return
	}
	if a.configPath == "" {
		a.saveAsConfiguration()
		return
	}
	if err := a.saveCurrentConfig(); err != nil {
		a.statusMessage = "Could not save configuration: " + err.Error()
	} else {
		a.statusMessage = ""
	}
	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *guiApp) applicationMenu() *mygo.Menu {
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	if runtime.GOOS == "darwin" {
		// Route the app menu's About command through the resizable in-app dialog.
		appMenu.Submenu = []*mygo.MenuItem{
			{ID: "about.application-menu", Label: "About " + branding.Name,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.showAbout() }},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}
	}
	fileItems := []*mygo.MenuItem{
		{ID: "config.new", Label: "New", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) { a.newConfiguration() }},
		{ID: "config.open", Label: "Open", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { a.openConfiguration() }},
		{ID: "config.save", Label: "Save", Accelerator: "CmdOrCtrl+S", Click: func(*mygo.MenuItem, *mygo.Window) { a.saveConfigurationFromMenu() }},
		{ID: "config.save-as", Label: "Save As", Accelerator: "CmdOrCtrl+Shift+S", Click: func(*mygo.MenuItem, *mygo.Window) { a.saveAsConfiguration() }},
		mygo.Separator(),
		{ID: "settings.open", Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}
	helpItems := []*mygo.MenuItem{
		{ID: "about.open", Label: "About", Click: func(*mygo.MenuItem, *mygo.Window) { a.showAbout() }},
	}
	items := []*mygo.MenuItem{
		appMenu,
		{Label: "File", Submenu: fileItems},
	}
	if runtime.GOOS == "darwin" {
		items = append(items, &mygo.MenuItem{Role: mygo.RoleEditMenu}, &mygo.MenuItem{Role: mygo.RoleWindowMenu})
	}
	items = append(items, &mygo.MenuItem{Label: "Help", Submenu: helpItems})
	return mygo.NewMenu(items)
}

func (a *guiApp) trayMenu() *mygo.Menu {
	a.trayAutoSelect = &mygo.MenuItem{
		ID: "tray.auto-select", Label: "Auto Select", Type: mygo.MenuItemCheckbox,
		Checked: a.server.AutoSelect(),
		ToolTip: "Allow clients to use every upstream identity.",
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			if item.IsChecked() {
				a.showMainWindow()
				a.confirmAutoSelect = true
				a.window.Invalidate()
				item.SetChecked(false)
				return
			}
			a.autoSelect = false
			a.server.SetAutoSelect(false)
			a.updateTrayAutoSelect()
			if a.window != nil {
				a.window.Invalidate()
			}
		},
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{ID: "tray.show", Label: "Show SSH KeySelect", Click: func(*mygo.MenuItem, *mygo.Window) { a.showMainWindow() }},
		mygo.Separator(),
		a.trayAutoSelect,
		{ID: "tray.settings", Label: "Settings…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
}

func (a *guiApp) updateConfigurationMenu() {
	if a == nil {
		return
	}
	menu := mygo.App.Menu()
	if menu == nil {
		return
	}
	enabled := !a.applying
	for _, id := range []string{"config.new", "config.open", "config.save", "config.save-as", "settings.open"} {
		if item := menu.ItemByID(id); item != nil {
			item.SetEnabled(enabled)
		}
	}
}

// guiChooseConfigToOpen asks the native file dialog to choose a TOML file.
func guiChooseConfigToOpen(parent *mygo.Window, currentPath string) (string, error) {
	defaultPath := ""
	if currentPath != "" {
		defaultPath = filepath.Dir(currentPath)
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: parent, Title: "Open Configuration", DefaultPath: defaultPath,
		Filters: []mygo.FileFilter{{Name: "TOML configuration", Extensions: []string{"toml"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// guiChooseConfigToSave uses the native save dialog and keeps TOML file names explicit.
func guiChooseConfigToSave(parent *mygo.Window, currentPath string) (string, error) {
	defaultPath := currentPath
	if defaultPath == "" {
		var err error
		defaultPath, err = guiDefaultConfigDirectory()
		if err != nil {
			return "", err
		}
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: parent, Title: "Save Configuration", DefaultPath: defaultPath,
		Filters: []mygo.FileFilter{{Name: "TOML configuration", Extensions: []string{"toml"}}},
	})
	if err != nil || path == "" {
		return "", err
	}
	ext := filepath.Ext(path)
	if ext == "" {
		path += ".toml"
	} else if !strings.EqualFold(ext, ".toml") {
		return "", fmt.Errorf("configuration file must use the .toml extension")
	}
	return path, nil
}

// guiDefaultConfigDirectory chooses the standard per-user folder for a new config file.
func guiDefaultConfigDirectory() (string, error) {
	var directory string
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locate user config directory: %w", err)
		}
		directory = filepath.Join(base, "ssh-keyselect")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate user home directory: %w", err)
		}
		directory = filepath.Join(home, ".config", "ssh-keyselect")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create default config directory: %w", err)
	}
	return directory, nil
}

// guiSettingsValues captures the controls so Apply can recognize an untouched dialog.
type guiSettingsValues struct {
	upstreamPath                     string
	listenPath                       string
	logFile                          string
	upstreamMode                     string
	listenMode                       string
	logLevel                         string
	theme                            string
	selectionTimeout                 float64
	allowUnboundSigningOnBindFailure bool
}

type guiSettingsState struct {
	app                              *guiApp
	window                           *mygo.Window
	cfg                              config.Config
	initialValues                    guiSettingsValues
	upstreamPath                     string
	listenPath                       string
	logFile                          string
	upstreamMode                     string
	listenMode                       string
	logLevel                         string
	theme                            string
	selectionTimeout                 float64
	allowUnboundSigningOnBindFailure bool
	initialListenMode                transport.Mode
	previousListenMode               string
	lastFilesystemPath               string
	applying                         bool
	browsingUpstream                 bool
	browsingListen                   bool
	browsingLog                      bool
	err                              string
}

func (a *guiApp) openSettings() {
	if a.applying {
		return
	}
	a.showMainWindow()
	state, err := newGUISettingsState(a, a.cfg)
	if err != nil {
		mygo.Dialog.Error("Could not open settings.", err.Error())
		return
	}
	// The Upstream checkbox adds one text line and section gap to the prior Settings content.
	height, minHeight := 550, 550
	switch runtime.GOOS {
	case "darwin":
		// Leave rounded-edge clearance without adding excess space around the actions.
		height, minHeight = 510, 510
	case "linux":
		height, minHeight = 490, 490
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "Settings", Parent: a.window, Modal: true,
		Width: 600, Height: height, MinWidth: 580, MinHeight: minHeight,
		Content: ui.View(state.view),
	})
	state.window = window
	if icon, iconErr := guiassets.WindowIconPNG(); iconErr == nil {
		_ = window.SetIcon(icon)
	}
	window.OnClose(func(event *mygo.CloseEvent) {
		if state.applying {
			event.PreventDefault()
		}
	})
}

func newGUISettingsState(app *guiApp, cfg config.Config) (*guiSettingsState, error) {
	listenEndpoint := cfg.Agent.Listen
	if listenEndpoint == "" {
		defaultPath, _, err := guiDefaultListenEndpoint(cfg.Agent.Upstream, cfg.Agent.ListenMode)
		if err != nil {
			return nil, err
		}
		listenEndpoint = defaultPath
	}
	initialListenMode := cfg.Agent.ListenMode
	if initialListenMode == transport.Auto {
		resolvedMode, err := listener.ResolveMode(listenEndpoint, cfg.Agent.Upstream, transport.Auto)
		if err != nil {
			return nil, err
		}
		initialListenMode = resolvedMode
		if initialListenMode == transport.Auto {
			initialListenMode = transport.Unix
		}
	}
	listenMode := guiDisplayModeName(initialListenMode)
	upstreamMode := guiDisplayModeName(cfg.Agent.UpstreamMode)
	state := &guiSettingsState{
		app: app, cfg: cfg,
		upstreamPath: guiDisplayEndpointPath(cfg.Agent.Upstream),
		listenPath:   guiDisplayEndpointPath(listenEndpoint),
		logFile:      guiDisplayFilePath(cfg.Log.File),
		upstreamMode: upstreamMode, listenMode: listenMode, logLevel: strings.ToLower(cfg.Log.Level),
		theme:                            guiThemeLabel(cfg.GUI.Theme),
		selectionTimeout:                 float64(cfg.Agent.SelectionTimeout),
		allowUnboundSigningOnBindFailure: cfg.Agent.AllowUnboundSigningOnBindFailure,
		initialListenMode:                initialListenMode, previousListenMode: listenMode,
		lastFilesystemPath: func() string {
			if guiEndpointPathIsPipe(listenEndpoint) {
				return ""
			}
			return listenEndpoint
		}(),
	}
	state.initialValues = state.values()
	return state, nil
}

func (s *guiSettingsState) values() guiSettingsValues {
	return guiSettingsValues{
		upstreamPath: s.upstreamPath, listenPath: s.listenPath, logFile: s.logFile,
		upstreamMode: s.upstreamMode, listenMode: s.listenMode, logLevel: s.logLevel,
		theme: s.theme, selectionTimeout: s.selectionTimeout,
		allowUnboundSigningOnBindFailure: s.allowUnboundSigningOnBindFailure,
	}
}

func (s *guiSettingsState) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) && s.window != nil {
		guiCloseWindowAfterFrame(s.window)
		return
	}
	theme := guitable.CompactTheme(c)
	edgePadding := guiDialogEdgePadding(theme)
	topPadding := theme.Space(1.5)
	if runtime.GOOS == "darwin" {
		topPadding = edgePadding
	}
	ui.Column(c).Fill().Padding(topPadding, edgePadding, theme.Space(1.5), edgePadding).
		Gap(theme.Space(1.5)).Background(theme.Background).Children(func() {
		s.endpointSection(c, theme, "Upstream", true)
		s.endpointSection(c, theme, "Listen", false)
		s.selectionSection(c, theme)
		s.loggingSection(c, theme)
		s.themeSection(c, theme)
		if s.err != "" {
			ui.Text(c, s.err).TextColor(theme.Danger)
		}
		if s.applying {
			ui.Text(c, "Applying configuration…").TextColor(theme.TextMuted)
		}
		guiDialogActionFooter(c, theme, func() {
			cancel := guiDialogActionButton(c, "Cancel", false, s.applying)
			if cancel.Clicked() {
				guiCloseWindowAfterFrame(s.window)
			}
			apply := guiDialogActionButton(c, "Apply", true, s.applying || s.browsingListen || s.browsingUpstream || s.browsingLog)
			if apply.Clicked() {
				s.apply()
			}
		})
	})
}

// themeSection lets the user choose the GUI theme saved in config.toml.
func (s *guiSettingsState) themeSection(c *ui.Context, theme *ui.Theme) {
	section := ui.Column(c).Shrink(0).Gap(theme.Space(1)).
		Padding(theme.Space(1.5), theme.Space(2)).
		Border(1, theme.Border).Radius(theme.Space(2)).Background(theme.Surface)
	section.Children(func() {
		ui.Text(c, "Theme").FontWeight(500)
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Select(c, &s.theme, []string{"Light", "Dark"}).Label("Theme").Grow(1)
		})
	})
}

// selectionSection controls the deadline for a pending interactive key choice.
func (s *guiSettingsState) selectionSection(c *ui.Context, theme *ui.Theme) {
	section := ui.Column(c).Shrink(0).Gap(theme.Space(1)).
		Padding(theme.Space(1.5), theme.Space(2)).
		Border(1, theme.Border).Radius(theme.Space(2)).Background(theme.Surface)
	section.Children(func() {
		ui.Text(c, "Key selection").FontWeight(500)
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Timeout").Width(theme.Space(26)).Shrink(0)
			// Keep 86400 editable so Apply can report the invalid endpoint instead of reverting it.
			ui.NumberInput(c, &s.selectionTimeout, 1, 86400, 1).Label("Key selection timeout")
			ui.Text(c, "seconds").TextColor(theme.TextMuted)
		})
	})
}

// endpointSection keeps each socket path and transport choice in a compact group.
func (s *guiSettingsState) endpointSection(c *ui.Context, theme *ui.Theme, title string, upstream bool) {
	path, mode := &s.listenPath, &s.listenMode
	browsing := s.browsingListen
	if upstream {
		path, mode, browsing = &s.upstreamPath, &s.upstreamMode, s.browsingUpstream
	}
	section := ui.Column(c).Shrink(0).Gap(theme.Space(1)).
		Padding(theme.Space(1.5), theme.Space(2)).
		Border(1, theme.Border).Radius(theme.Space(2)).Background(theme.Surface)
	section.Children(func() {
		ui.Text(c, title).FontWeight(500)
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Socket path").Width(theme.Space(26)).Shrink(0)
			input := ui.TextInput(c, path).Label("Socket path").Grow(1)
			if upstream {
				input.Placeholder("Leave blank to configure later")
			}
			if input.Changed() {
				*path = guiFormatPathInput(*path)
			}
			browse := ui.Button(c, "Browse…").Disabled(guiEndpointPathIsPipe(*path) || browsing || guiModeFromDisplay(*mode) == transport.NamedPipe)
			if browse.Clicked() {
				s.browse(upstream)
			}
		})
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Mode").Width(theme.Space(26)).Shrink(0)
			if ui.Select(c, mode, guiTransportModeOptions(upstream)).Label("Mode").Grow(1).Changed() {
				if upstream {
					s.changeUpstreamMode()
				} else {
					s.changeListenMode()
				}
			}
		})
		if upstream {
			ui.Checkbox(c, &s.allowUnboundSigningOnBindFailure, "Allow signing after upstream rejects session binding (Required for 1Password)")
		}
	})
}

// loggingSection exposes both the optional file destination and the active log level.
func (s *guiSettingsState) loggingSection(c *ui.Context, theme *ui.Theme) {
	section := ui.Column(c).Shrink(0).Gap(theme.Space(1)).
		Padding(theme.Space(1.5), theme.Space(2)).
		Border(1, theme.Border).Radius(theme.Space(2)).Background(theme.Surface)
	section.Children(func() {
		ui.Text(c, "Logging").FontWeight(500)
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Log file").Width(theme.Space(26)).Shrink(0)
			input := ui.TextInput(c, &s.logFile).Label("Log file").Placeholder("Leave blank to log to standard error").Grow(1)
			if input.Changed() {
				s.logFile = guiFormatPathInput(s.logFile)
			}
			browse := ui.Button(c, "Browse…").Disabled(s.browsingLog)
			if browse.Clicked() {
				s.browseLogFile()
			}
		})
		ui.Row(c).Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Level").Width(theme.Space(26)).Shrink(0)
			ui.Select(c, &s.logLevel, []string{"off", "debug", "info", "warn", "error"}).Label("Log level").Grow(1)
		})
	})
}

func (s *guiSettingsState) changeListenMode() {
	selected := guiModeFromDisplay(s.listenMode)
	previous := guiModeFromDisplay(s.previousListenMode)
	upstreamEndpoint, err := s.upstreamEndpoint()
	if err != nil {
		s.listenMode = s.previousListenMode
		s.err = "Could not resolve the upstream endpoint: " + err.Error()
		return
	}
	switch {
	case selected == transport.NamedPipe && !guiEndpointPathIsPipe(s.listenPath):
		if !guiEndpointPathIsPipe(s.listenPath) {
			s.lastFilesystemPath, err = guiEndpointPathFromDialog(
				guiEndpointPathFromDisplay(s.listenPath), previous, upstreamEndpoint)
			if err != nil {
				s.listenMode = s.previousListenMode
				s.err = "Could not preserve the filesystem socket path: " + err.Error()
				return
			}
		}
		path, _, pathErr := guiDefaultListenEndpoint(upstreamEndpoint, transport.NamedPipe)
		if pathErr != nil {
			s.listenMode = s.previousListenMode
			s.err = "Could not create an OpenSSH pipe endpoint: " + pathErr.Error()
			return
		}
		s.listenPath = guiDisplayEndpointPath(path)
	case selected != transport.NamedPipe && guiEndpointPathIsPipe(s.listenPath):
		path := s.lastFilesystemPath
		if path == "" {
			path, _, err = guiDefaultListenEndpoint(upstreamEndpoint, selected)
		} else {
			path, err = guiEndpointPathFromDialog(guiEndpointPathForDialog(path), selected, upstreamEndpoint)
		}
		if err != nil {
			s.listenMode = s.previousListenMode
			s.err = "Could not restore a filesystem socket endpoint: " + err.Error()
			return
		}
		s.listenPath = guiDisplayEndpointPath(path)
		s.lastFilesystemPath = path
	case selected != transport.NamedPipe:
		s.lastFilesystemPath, err = guiEndpointPathFromDialog(
			guiEndpointPathFromDisplay(s.listenPath), selected, upstreamEndpoint)
		if err != nil {
			s.listenMode = s.previousListenMode
			s.err = "Could not update the filesystem socket path: " + err.Error()
			return
		}
	}
	s.previousListenMode = s.listenMode
	s.err = ""
}

// changeUpstreamMode updates the default agent path to match the selected transport.
func (s *guiSettingsState) changeUpstreamMode() {
	endpoint, err := guiDefaultUpstreamEndpoint(guiModeFromDisplay(s.upstreamMode))
	if err != nil {
		s.err = "Could not create an upstream endpoint: " + err.Error()
		return
	}
	s.upstreamPath = guiDisplayEndpointPath(endpoint)
	s.err = ""
}

func (s *guiSettingsState) upstreamEndpoint() (string, error) {
	if s.upstreamPath == "" {
		return "", nil
	}
	return guiEndpointPathFromDialog(
		guiEndpointPathFromDisplay(s.upstreamPath),
		guiModeFromDisplay(s.upstreamMode),
		s.cfg.Agent.Upstream,
	)
}

func (s *guiSettingsState) buildConfig() (config.Config, error) {
	if math.Trunc(s.selectionTimeout) != s.selectionTimeout {
		return config.Config{}, fmt.Errorf("key selection timeout must be a whole number of seconds")
	}
	cfg := s.cfg
	upstreamMode := guiModeFromDisplay(s.upstreamMode)
	upstreamEndpoint := ""
	if s.upstreamPath != "" {
		if s.upstreamPath == guiDisplayEndpointPath(cfg.Agent.Upstream) && upstreamMode == cfg.Agent.UpstreamMode {
			upstreamEndpoint = cfg.Agent.Upstream
		} else {
			var err error
			upstreamEndpoint, err = guiEndpointPathFromDialog(
				guiEndpointPathFromDisplay(s.upstreamPath), upstreamMode, cfg.Agent.Upstream)
			if err != nil {
				return config.Config{}, err
			}
		}
	}
	listenMode := guiModeFromDisplay(s.listenMode)
	if cfg.Agent.ListenMode == transport.Auto && listenMode == s.initialListenMode {
		listenMode = transport.Auto
	}
	listenEndpoint := ""
	if cfg.Agent.Listen != "" && s.listenPath == guiDisplayEndpointPath(cfg.Agent.Listen) && listenMode == cfg.Agent.ListenMode {
		listenEndpoint = cfg.Agent.Listen
	} else {
		var err error
		listenEndpoint, err = guiEndpointPathFromDialog(
			guiEndpointPathFromDisplay(s.listenPath), listenMode, upstreamEndpoint)
		if err != nil {
			return config.Config{}, err
		}
	}
	cfg.Agent.Upstream = upstreamEndpoint
	cfg.Agent.UpstreamMode = upstreamMode
	cfg.Agent.Listen = listenEndpoint
	cfg.Agent.ListenMode = listenMode
	cfg.Agent.SelectionTimeout = int(s.selectionTimeout)
	cfg.Agent.AllowUnboundSigningOnBindFailure = s.allowUnboundSigningOnBindFailure
	cfg.Log.File = config.ExpandPath(guiFilePathFromDisplay(s.logFile))
	cfg.Log.Level = s.logLevel
	cfg.GUI.Theme = guiThemeValue(s.theme)
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// browseLogFile chooses where the GUI appends its diagnostic log.
func (s *guiSettingsState) browseLogFile() {
	s.browsingLog = true
	window := s.window
	current := guiFilePathFromDisplay(s.logFile)
	go func() {
		path, err := guiChooseLogFile(window, current)
		if window == nil || window.IsDestroyed() {
			return
		}
		window.Update(func() {
			s.browsingLog = false
			if err != nil {
				s.err = err.Error()
			} else if path != "" {
				s.logFile = guiDisplayFilePath(path)
				s.err = ""
			}
		})
	}()
}

// guiChooseLogFile uses the native save dialog and adds the conventional suffix when omitted.
func guiChooseLogFile(parent *mygo.Window, currentPath string) (string, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: parent, Title: "Choose a log file", DefaultPath: currentPath,
		Filters: []mygo.FileFilter{{Name: "Log files", Extensions: []string{"log"}}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += ".log"
	}
	return path, nil
}

func (s *guiSettingsState) apply() {
	cfg, err := s.buildConfig()
	if err != nil {
		s.err = err.Error()
		return
	}
	s.err = ""
	// Derived defaults can make buildConfig differ even when the dialog is untouched.
	if s.values() == s.initialValues || cfg == s.cfg {
		guiCloseWindowAfterFrame(s.window)
		return
	}
	s.applying = true
	s.app.applyConfig(cfg, true, func(err error) {
		s.applying = false
		if err != nil {
			s.err = err.Error()
			s.window.Invalidate()
			return
		}
		guiCloseWindowAfterFrame(s.window)
	})
}

func (s *guiSettingsState) browse(upstream bool) {
	path, mode, endpoint := s.listenPath, guiModeFromDisplay(s.listenMode), ""
	if upstream {
		path, mode = s.upstreamPath, guiModeFromDisplay(s.upstreamMode)
	} else {
		var err error
		endpoint, err = s.upstreamEndpoint()
		if err != nil {
			s.err = err.Error()
			return
		}
	}
	if upstream {
		s.browsingUpstream = true
	} else {
		s.browsingListen = true
	}
	window := s.window
	go func() {
		chosen, ok, err := guiChooseEndpointPath(window, path, mode, endpoint, !upstream)
		if window == nil || window.IsDestroyed() {
			return
		}
		window.Update(func() {
			s.browsingUpstream = false
			s.browsingListen = false
			if err != nil {
				s.err = err.Error()
			} else if ok {
				if upstream {
					s.upstreamPath = guiDisplayEndpointPath(chosen)
				} else {
					s.listenPath = guiDisplayEndpointPath(chosen)
					s.lastFilesystemPath = chosen
				}
				s.err = ""
			}
		})
	}()
}

func guiTransportModeOptions(includeAuto bool) []string {
	modes := []transport.Mode{transport.Cygwin, transport.NamedPipe, transport.Unix, transport.WSL1}
	options := make([]string, 0, len(modes)+1)
	if includeAuto {
		options = append(options, guiDisplayModeName(transport.Auto))
	}
	for _, mode := range modes {
		options = append(options, guiDisplayModeName(mode))
	}
	return options
}

func guiModeFromDisplay(label string) transport.Mode {
	for _, mode := range []transport.Mode{transport.Auto, transport.Cygwin, transport.NamedPipe, transport.Unix, transport.WSL1} {
		if guiDisplayModeName(mode) == label {
			return mode
		}
	}
	return transport.Auto
}

func guiChooseEndpointPath(parent *mygo.Window, current string, mode transport.Mode, upstream string, save bool) (string, bool, error) {
	initialPath := guiEndpointPathForDialog(guiEndpointPathFromDisplay(current))
	initialDirectory := guiEndpointDialogInitialDirectory(initialPath)
	if save {
		name := filepath.Base(initialPath)
		if initialPath == "" || name == "." || name == string(filepath.Separator) {
			name = "ssh-keyselect-agent.sock"
		}
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: parent, Title: "Choose a socket path",
			DefaultPath: filepath.Join(initialDirectory, name),
			ButtonLabel: "Choose",
		})
		if err != nil || path == "" {
			return "", false, err
		}
		resolved, err := guiEndpointPathFromDialog(path, mode, upstream)
		return resolved, err == nil, err
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: parent, Title: "Choose a socket path",
		DefaultPath: initialDirectory,
	})
	if err != nil || len(paths) == 0 {
		return "", false, err
	}
	resolved, err := guiEndpointPathFromDialog(paths[0], mode, upstream)
	return resolved, err == nil, err
}

func guiEndpointDialogInitialDirectory(path string) string {
	if path == "" {
		return ""
	}
	directory := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		directory = filepath.Dir(path)
	}
	for directory != "" && directory != "." {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return ""
}
