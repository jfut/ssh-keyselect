//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/mod"
	"github.com/richardwilkes/unison/enums/paintstyle"
	"github.com/richardwilkes/unison/enums/role"
)

type guiFileMenuActions struct {
	open     func()
	save     func()
	saveAs   func()
	settings func()
}

type guiConfigState struct {
	cfg                 config.Config
	filePath            string
	dirty               bool
	actualListen        string
	effectiveListenMode transport.Mode
	runtime             *guiRuntime
	onUpdate            func()
	onDirtyChange       func()
}

// configForDisk stores endpoint paths in the native format for this OS.
func guiConfigForDisk(cfg config.Config) config.Config {
	cfg.Agent.Listen = guiListenPathForConfig(cfg.Agent.Listen)
	return cfg
}

func (s *guiConfigState) apply(cfg config.Config, dirty bool) error {
	actualListen, effectiveMode, err := s.runtime.Apply(cfg)
	if err != nil {
		return err
	}
	cfg.Agent.Upstream = config.ExpandPath(cfg.Agent.Upstream)
	cfg.Agent.Listen = config.ExpandPath(cfg.Agent.Listen)
	s.cfg = cfg
	s.actualListen = actualListen
	s.effectiveListenMode = effectiveMode
	s.setDirty(dirty)
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}

func (s *guiConfigState) setDirty(dirty bool) {
	if s.dirty == dirty {
		return
	}
	s.dirty = dirty
	if s.onDirtyChange != nil {
		s.onDirtyChange()
	}
}

func (s *guiConfigState) save() error {
	if err := config.Save(s.filePath, guiConfigForDisk(s.cfg)); err != nil {
		return err
	}
	s.setDirty(false)
	return nil
}

func (s *guiConfigState) open() {
	path, ok := guiChooseConfigToOpen(s.filePath)
	if !ok {
		return
	}
	if s.dirty {
		switch guiConfirmSaveBeforeOpen(s.filePath) {
		case unison.ModalResponseOK:
			if err := s.save(); err != nil {
				showGUIErrorDialog("Could not save configuration.", err)
				return
			}
		case unison.ModalResponseDiscard:
		default:
			return
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		showGUIErrorDialog("Could not open configuration.", err)
		return
	}
	if err := s.apply(cfg, false); err != nil {
		showGUIErrorDialog("Could not apply configuration.", err)
		return
	}
	s.filePath = path
}

func (s *guiConfigState) saveAs() {
	path, ok := guiChooseConfigToSave(s.filePath)
	if !ok {
		return
	}
	if err := config.Save(path, guiConfigForDisk(s.cfg)); err != nil {
		showGUIErrorDialog("Could not save configuration.", err)
		return
	}
	s.filePath = path
	s.setDirty(false)
}

// confirmSaveBeforeOpen asks whether to save before replacing the active configuration.
func guiConfirmSaveBeforeOpen(path string) int {
	return guiRunUnsavedChangesDialog("Save changes before opening another configuration?", path, []*unison.DialogButtonInfo{
		unison.NewCancelButtonInfo(),
		unison.NewNoButtonInfo(),
		unison.NewYesButtonInfo(),
	})
}

// confirmSaveBeforeClose puts the close actions in Yes, No, Cancel order.
func guiConfirmSaveBeforeClose(path string) int {
	return guiRunUnsavedChangesDialog("Save changes before closing?", path, []*unison.DialogButtonInfo{
		unison.NewYesButtonInfo(),
		unison.NewNoButtonInfo(),
		unison.NewCancelButtonInfo(),
	})
}

func guiRunUnsavedChangesDialog(message, path string, buttons []*unison.DialogButtonInfo) int {
	messagePanel := unison.NewMessagePanel(message, guiDisplayEndpointPath(path))
	guiSetMessagePanelRegularFonts(messagePanel)
	dialog, err := newGUIDialog(
		branding.Name,
		unison.DefaultDialogTheme.QuestionIcon,
		unison.DefaultDialogTheme.QuestionIconInk,
		messagePanel,
		buttons,
	)
	if err != nil {
		return unison.ModalResponseCancel
	}
	return dialog.RunModal()
}

// setMessagePanelRegularFonts gives save prompts regular GUI text without changing their font sizes.
func guiSetMessagePanelRegularFonts(panel *unison.Panel) {
	for _, child := range panel.Children() {
		label, ok := child.Self.(*unison.Label)
		if !ok {
			continue
		}
		title := label.String()
		label.Font = unison.LabelFont.Face().Font(label.Font.Size())
		label.SetTitle(title)
	}
}

func (s *guiConfigState) saveFromMenu() {
	if err := s.save(); err != nil {
		showGUIErrorDialog("Could not save configuration.", err)
	}
}

// menuItemContentWidth measures the part of a menu item occupied by its title and shortcut.
func guiMenuItemContentWidth(item unison.MenuItem) float32 {
	theme := unison.DefaultMenuItemTheme
	width := unison.NewText(item.Title(), &unison.TextDecoration{Font: theme.TitleFont}).Width()
	binding := item.KeyBinding()
	if binding.KeyCode != 0 {
		keys := binding.String()
		if keys != "" {
			width += theme.KeyGap + unison.NewText(keys, &unison.TextDecoration{Font: theme.KeyFont}).Width()
		}
	}
	return width
}

// padMenuTitleToWidth gives an in-window menu title enough invisible trailing space to match a wider menu.
func guiPadMenuTitleToWidth(title string, width float32) string {
	font := unison.DefaultMenuItemTheme.TitleFont
	for unison.NewText(title, &unison.TextDecoration{Font: font}).Width() < width {
		title += " "
	}
	return title
}

// styleGUIInWindowMenuBar paints the menu bar's unused space white, including the scroll panel behind its menus.
func styleGUIInWindowMenuBar(root *unison.Panel) {
	if root == nil {
		return
	}
	for _, child := range root.Children() {
		if child.Accessibility.Role != role.MenuBar {
			continue
		}
		child.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
			canvas.DrawRect(rect, guiMenuInk.Paint(canvas, rect, paintstyle.Fill))
		}
		for _, barChild := range child.Children() {
			if scroll, ok := barChild.Self.(*unison.ScrollPanel); ok {
				scroll.BackgroundInk = guiMenuInk
				break
			}
		}
		break
	}
}

// installGUIFileMenu adds configuration file actions alongside Exit and About.
func installGUIFileMenu(window *unison.Window, actions guiFileMenuActions) {
	factory := unison.DefaultMenuFactory()
	if root := window.Content().Parent(); root != nil {
		root.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
			canvas.DrawRect(rect, guiCardInk.Paint(canvas, rect, paintstyle.Fill))
		}
	}
	factory.BarForWindow(window, func(bar unison.Menu) {
		file := factory.NewMenu(unison.UserBaseID+20, "File", nil)
		file.InsertItem(-1, factory.NewItem(unison.UserBaseID+24, "Open...", unison.KeyBinding{KeyCode: unison.KeyO, Modifiers: mod.Control}, nil,
			func(unison.MenuItem) { actions.open() }))
		file.InsertItem(-1, factory.NewItem(unison.UserBaseID+25, "Save", unison.KeyBinding{KeyCode: unison.KeyS, Modifiers: mod.Control}, nil,
			func(unison.MenuItem) { actions.save() }))
		file.InsertItem(-1, factory.NewItem(unison.UserBaseID+26, "Save As...", unison.KeyBinding{KeyCode: unison.KeyS, Modifiers: mod.Control | mod.Shift}, nil,
			func(unison.MenuItem) { actions.saveAs() }))
		file.InsertSeparator(-1, true)
		file.InsertItem(-1, factory.NewItem(unison.UserBaseID+27, "Settings", unison.KeyBinding{}, nil,
			func(unison.MenuItem) { actions.settings() }))
		file.InsertSeparator(-1, true)
		file.InsertItem(-1, factory.NewItem(unison.UserBaseID+21, "Exit", unison.KeyBinding{}, nil,
			func(unison.MenuItem) { unison.AttemptQuit() }))
		bar.InsertMenu(-1, file)

		help := factory.NewMenu(unison.UserBaseID+22, "Help", nil)
		aboutTitle := "About"
		if factory.BarIsPerWindow() {
			var widestFileItem float32
			for i := range file.Count() {
				if item := file.ItemAtIndex(i); !item.IsSeparator() {
					widestFileItem = max(widestFileItem, guiMenuItemContentWidth(item))
				}
			}
			aboutTitle = guiPadMenuTitleToWidth(aboutTitle, widestFileItem)
		}
		help.InsertItem(-1, factory.NewItem(unison.UserBaseID+23, aboutTitle, unison.KeyBinding{}, nil,
			func(unison.MenuItem) { showGUIAboutDialog() }))
		bar.InsertMenu(-1, help)
	})
	if factory.BarIsPerWindow() {
		if root := window.Content().Parent(); root != nil {
			styleGUIInWindowMenuBar(root)
		}
	}
}

// chooseConfigToOpen asks for a TOML file to load.
func guiChooseConfigToOpen(currentPath string) (string, bool) {
	dialog := unison.NewOpenDialog()
	dialog.SetCanChooseFiles(true)
	dialog.SetCanChooseDirectories(false)
	dialog.SetAllowedExtensions("toml")
	if currentPath != "" {
		dialog.SetInitialDirectory(filepath.Dir(currentPath))
	}
	if !dialog.RunModal() {
		return "", false
	}
	return dialog.Path(), true
}

// chooseConfigToSave asks where to save a TOML file and normalizes its extension.
func guiChooseConfigToSave(currentPath string) (string, bool) {
	dialog := unison.NewSaveDialog()
	dialog.SetAllowedExtensions("toml")
	if currentPath != "" {
		dialog.SetInitialDirectory(filepath.Dir(currentPath))
		dialog.SetInitialFileName(filepath.Base(currentPath))
	}
	if !dialog.RunModal() {
		return "", false
	}
	path, ok := unison.ValidateSaveFilePath(dialog.Path(), "toml", false)
	return path, ok
}

// editGUIEndpointSettings groups each endpoint's path and transport settings in the Settings dialog.
func editGUIEndpointSettings(cfg config.Config) (config.Config, bool, error) {
	content := unison.NewPanel()
	content.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 10})
	content.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})

	upstreamPath := unison.NewField()
	upstreamPath.Watermark = "Leave blank to configure later"
	upstreamPath.SetText(guiDisplayEndpointPath(cfg.Agent.Upstream))
	upstreamMode := guiNewTransportModePopup(cfg.Agent.UpstreamMode, true)
	listenPath := unison.NewField()
	listenEndpoint := cfg.Agent.Listen
	if cfg.Agent.Listen == "" {
		defaultPath, _, err := guiDefaultListenEndpoint(cfg.Agent.Upstream, cfg.Agent.ListenMode)
		if err != nil {
			return config.Config{}, false, err
		}
		listenEndpoint = defaultPath
		listenPath.SetText(guiDisplayEndpointPath(defaultPath))
	} else {
		listenPath.SetText(guiDisplayEndpointPath(cfg.Agent.Listen))
	}
	initialListenMode := cfg.Agent.ListenMode
	if initialListenMode == transport.Auto {
		resolvedMode, err := listener.ResolveMode(listenEndpoint, cfg.Agent.Upstream, transport.Auto)
		if err != nil {
			return config.Config{}, false, err
		}
		initialListenMode = resolvedMode
		if initialListenMode == transport.Auto {
			initialListenMode = transport.Unix
		}
	}
	listenMode := guiNewTransportModePopup(initialListenMode, false)

	upstreamGroup := newGUIEndpointConfigGroup("Upstream")
	upstreamRows := newGUIEndpointConfigRows()
	upstreamBrowse := guiNewEndpointBrowseButton()
	upstreamPathControls := guiNewEndpointPathControls(upstreamPath, upstreamBrowse)
	addGUIConfigRow(upstreamRows, "Socket path", upstreamPathControls)
	addGUIConfigRow(upstreamRows, "Mode", upstreamMode)
	upstreamGroup.AddChild(upstreamRows)
	content.AddChild(upstreamGroup)

	listenGroup := newGUIEndpointConfigGroup("Listen")
	listenRows := newGUIEndpointConfigRows()
	listenBrowse := guiNewEndpointBrowseButton()
	listenPathControls := guiNewEndpointPathControls(listenPath, listenBrowse)
	addGUIConfigRow(listenRows, "Socket path", listenPathControls)
	addGUIConfigRow(listenRows, "Mode", listenMode)
	listenGroup.AddChild(listenRows)
	content.AddChild(listenGroup)

	// Keep a usable filesystem path when users switch to and from the named-pipe transport.
	lastFilesystemListenPath := ""
	if !guiEndpointPathIsPipe(listenEndpoint) {
		lastFilesystemListenPath = listenEndpoint
	}
	previousListenMode := guiSelectedModeValue(listenMode)
	resettingListenMode := false
	currentUpstreamEndpoint := func() (string, error) {
		return guiEndpointPathFromDialog(guiEndpointPathFromDisplay(upstreamPath.Text()), guiSelectedModeValue(upstreamMode), cfg.Agent.Upstream)
	}
	updateUpstreamBrowse := func() {
		upstreamBrowse.SetEnabled(!guiEndpointPathIsPipe(upstreamPath.Text()) && guiSelectedModeValue(upstreamMode) != transport.NamedPipe)
	}
	upstreamMode.SelectionChangedCallback = func(*unison.PopupMenu[string]) { updateUpstreamBrowse() }
	upstreamPath.ModifiedCallback = func(_, _ *unison.FieldState) { updateUpstreamBrowse() }

	upstreamBrowse.ClickCallback = func() {
		path, ok, err := guiChooseEndpointPath(upstreamPath.Text(), guiSelectedModeValue(upstreamMode), "", false)
		if err != nil {
			showGUIErrorDialog("Could not select upstream socket path.", err)
			return
		}
		if ok {
			upstreamPath.SetText(guiDisplayEndpointPath(path))
		}
	}
	listenBrowse.ClickCallback = func() {
		upstreamEndpoint, err := currentUpstreamEndpoint()
		if err != nil {
			showGUIErrorDialog("Could not resolve the upstream endpoint.", err)
			return
		}
		path, ok, err := guiChooseEndpointPath(listenPath.Text(), guiSelectedModeValue(listenMode), upstreamEndpoint, true)
		if err != nil {
			showGUIErrorDialog("Could not select listen socket path.", err)
			return
		}
		if ok {
			listenPath.SetText(guiDisplayEndpointPath(path))
			lastFilesystemListenPath = path
		}
	}

	updateListenBrowse := func() {
		listenBrowse.SetEnabled(!guiEndpointPathIsPipe(listenPath.Text()) && guiSelectedModeValue(listenMode) != transport.NamedPipe)
	}
	listenPath.ModifiedCallback = func(_, _ *unison.FieldState) { updateListenBrowse() }
	listenMode.SelectionChangedCallback = func(popup *unison.PopupMenu[string]) {
		if resettingListenMode {
			return
		}
		selected := guiSelectedModeValue(popup)
		upstreamEndpoint, err := currentUpstreamEndpoint()
		if err != nil {
			showGUIErrorDialog("Could not resolve the upstream endpoint.", err)
			resettingListenMode = true
			popup.Select(string(previousListenMode))
			resettingListenMode = false
			return
		}
		if selected == transport.NamedPipe && !guiEndpointPathIsPipe(listenPath.Text()) {
			lastFilesystemListenPath, err = guiEndpointPathFromDialog(guiEndpointPathFromDisplay(listenPath.Text()), previousListenMode, upstreamEndpoint)
			if err != nil {
				showGUIErrorDialog("Could not preserve the filesystem socket path.", err)
				resettingListenMode = true
				popup.Select(string(previousListenMode))
				resettingListenMode = false
				return
			}
			pipe, _, err := guiDefaultListenEndpoint(upstreamEndpoint, transport.NamedPipe)
			if err != nil {
				showGUIErrorDialog("Could not create an OpenSSH pipe endpoint.", err)
				resettingListenMode = true
				popup.Select(string(previousListenMode))
				resettingListenMode = false
				return
			}
			listenPath.SetText(guiDisplayEndpointPath(pipe))
		} else if selected != transport.NamedPipe && guiEndpointPathIsPipe(listenPath.Text()) {
			path := lastFilesystemListenPath
			if path == "" {
				path, _, err = guiDefaultListenEndpoint(upstreamEndpoint, selected)
				if err != nil {
					showGUIErrorDialog("Could not create a filesystem socket endpoint.", err)
					resettingListenMode = true
					popup.Select(string(previousListenMode))
					resettingListenMode = false
					return
				}
			} else {
				path, err = guiEndpointPathFromDialog(guiEndpointPathForDialog(path), selected, upstreamEndpoint)
				if err != nil {
					showGUIErrorDialog("Could not restore the filesystem socket path.", err)
					resettingListenMode = true
					popup.Select(string(previousListenMode))
					resettingListenMode = false
					return
				}
			}
			listenPath.SetText(guiDisplayEndpointPath(path))
			lastFilesystemListenPath = path
		} else if selected != transport.NamedPipe {
			lastFilesystemListenPath, err = guiEndpointPathFromDialog(guiEndpointPathFromDisplay(listenPath.Text()), selected, upstreamEndpoint)
			if err != nil {
				showGUIErrorDialog("Could not update the filesystem socket path.", err)
				resettingListenMode = true
				popup.Select(string(previousListenMode))
				resettingListenMode = false
				return
			}
		}
		previousListenMode = selected
		updateListenBrowse()
	}
	updateUpstreamBrowse()
	updateListenBrowse()

	dialog, err := newGUIDialog("Settings", nil, nil, content, []*unison.DialogButtonInfo{
		unison.NewCancelButtonInfo(),
		unison.NewOKButtonInfoWithTitle("Apply"),
	})
	if err != nil {
		return config.Config{}, false, err
	}
	if dialog.RunModal() != unison.ModalResponseOK {
		return config.Config{}, false, nil
	}
	upstreamModeValue, ok := upstreamMode.Selected()
	if !ok {
		return config.Config{}, false, errGUINoTransportMode
	}
	listenModeValue, ok := listenMode.Selected()
	if !ok {
		return config.Config{}, false, errGUINoTransportMode
	}
	upstreamEndpoint := ""
	if cfg.Agent.Upstream != "" && upstreamPath.Text() == guiDisplayEndpointPath(cfg.Agent.Upstream) &&
		transport.Mode(upstreamModeValue) == cfg.Agent.UpstreamMode {
		upstreamEndpoint = cfg.Agent.Upstream
	} else {
		upstreamEndpoint, err = guiEndpointPathFromDialog(guiEndpointPathFromDisplay(upstreamPath.Text()), transport.Mode(upstreamModeValue), cfg.Agent.Upstream)
		if err != nil {
			return config.Config{}, false, err
		}
	}
	selectedListenMode := transport.Mode(listenModeValue)
	if cfg.Agent.ListenMode == transport.Auto && selectedListenMode == initialListenMode {
		selectedListenMode = transport.Auto
	}
	if cfg.Agent.Listen != "" && listenPath.Text() == guiDisplayEndpointPath(cfg.Agent.Listen) && selectedListenMode == cfg.Agent.ListenMode {
		listenEndpoint = cfg.Agent.Listen
	} else {
		listenEndpoint, err = guiEndpointPathFromDialog(guiEndpointPathFromDisplay(listenPath.Text()), selectedListenMode, upstreamEndpoint)
		if err != nil {
			return config.Config{}, false, err
		}
	}
	cfg.Agent.Upstream = upstreamEndpoint
	cfg.Agent.UpstreamMode = transport.Mode(upstreamModeValue)
	cfg.Agent.Listen = listenEndpoint
	cfg.Agent.ListenMode = selectedListenMode
	return cfg, true, nil
}

func guiChooseEndpointPath(current string, mode transport.Mode, upstream string, save bool) (string, bool, error) {
	initialPath := guiEndpointPathForDialog(guiEndpointPathFromDisplay(current))
	initialDirectory := guiEndpointDialogInitialDirectory(initialPath)
	if save {
		dialog := unison.NewSaveDialog()
		if initialDirectory != "" {
			dialog.SetInitialDirectory(initialDirectory)
		}
		name := filepath.Base(initialPath)
		if initialPath == "" || name == "." || name == string(filepath.Separator) {
			name = "ssh-keyselect-agent.sock"
		}
		dialog.SetInitialFileName(name)
		if !dialog.RunModal() {
			return "", false, nil
		}
		path, err := guiEndpointPathFromDialog(dialog.Path(), mode, upstream)
		return path, err == nil, err
	}
	dialog := unison.NewOpenDialog()
	dialog.SetCanChooseFiles(true)
	dialog.SetCanChooseDirectories(false)
	if initialDirectory != "" {
		dialog.SetInitialDirectory(initialDirectory)
	}
	if !dialog.RunModal() {
		return "", false, nil
	}
	path, err := guiEndpointPathFromDialog(dialog.Path(), mode, upstream)
	return path, err == nil, err
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

func guiSelectedModeValue(popup *unison.PopupMenu[string]) transport.Mode {
	value, ok := popup.Selected()
	if !ok {
		return transport.Auto
	}
	return transport.Mode(value)
}

func newGUIEndpointConfigGroup(title string) *unison.Panel {
	group := newGUIStatusCard()
	group.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
	heading := unison.NewLabel()
	heading.Font = guiFont(10, false)
	heading.OnBackgroundInk = guiTextInk
	heading.SetTitle(title)
	group.AddChild(heading)
	return group
}

func newGUIEndpointConfigRows() *unison.Panel {
	rows := unison.NewPanel()
	rows.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 12, VSpacing: 8, VAlign: align.Middle})
	rows.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
	return rows
}

func guiNewEndpointPathControls(field *unison.Field, browse *unison.Button) *unison.Panel {
	controls := unison.NewPanel()
	controls.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 6, VAlign: align.Middle})
	field.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true, MinSize: geom.NewSize(320, 0)})
	controls.AddChild(field)
	browse.SetLayoutData(&unison.FlexLayoutData{VAlign: align.Middle})
	controls.AddChild(browse)
	return controls
}

func guiNewEndpointBrowseButton() *unison.Button {
	button := unison.NewButton()
	button.Font = guiFont(10.5, false)
	button.SetTitle("Browse…")
	button.Tooltip = unison.NewTooltipWithText("Choose a socket path")
	return button
}

func guiNewTransportModePopup(current transport.Mode, includeAutomatic bool) *unison.PopupMenu[string] {
	popup := unison.NewPopupMenu[string]()
	// Keep the selected value and the expanded menu on the same font.
	popup.Font = unison.DefaultMenuItemTheme.TitleFont
	popup.ItemRendererCallback = func(mode string) string {
		return guiDisplayModeName(transport.Mode(mode))
	}
	items := []string{}
	if includeAutomatic {
		items = append(items, string(transport.Auto))
	}
	items = append(items,
		string(transport.Cygwin),
		string(transport.NamedPipe),
		string(transport.Unix),
		string(transport.WSL1),
	)
	popup.AddItem(items...)
	if popup.IndexOfItem(string(current)) < 0 {
		if includeAutomatic {
			current = transport.Auto
		} else {
			current = transport.Unix
		}
	}
	popup.Select(string(current))
	return popup
}

func addGUIConfigRow(panel *unison.Panel, title string, control unison.Paneler) {
	label := unison.NewLabel()
	label.Font = guiFont(10, false)
	label.OnBackgroundInk = guiTextInk
	label.SetTitle(title)
	panel.AddChild(label)
	control.AsPanel().SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
	panel.AddChild(control)
}

var errGUINoTransportMode = errors.New("transport mode is not selected")
