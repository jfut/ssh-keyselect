//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/guiidentitytable"
	"github.com/jfut/ssh-keyselect/internal/guistyle"
	"github.com/jfut/ssh-keyselect/internal/guiwindow"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/systemtray"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/mod"
	"github.com/richardwilkes/unison/enums/paintstyle"
)

// commandName is the CLI command shown in GUI startup errors.
const guiCommandName = "ssh-keyselect"

type guiOptions struct {
	Config       *string          `help:"TOML configuration file." placeholder:"FILE"`
	Listen       *string          `help:"Frontend endpoint (default: HOME socket, or the fixed Windows OpenSSH named-pipe endpoint)." placeholder:"ENDPOINT"`
	ListenMode   *string          `help:"Frontend protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	Upstream     *string          `help:"Upstream endpoint (default: UPSTREAM_SSH_AUTH_SOCK, then SSH_AUTH_SOCK)." placeholder:"ENDPOINT"`
	UpstreamMode *string          `help:"Upstream protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	LogLevel     *string          `help:"Log level (off, debug, info, warn, error)." placeholder:"LEVEL"`
	Version      kong.VersionFlag `help:"Print version information and quit."`
}

// executeGUI parses options and runs the GUI agent command.
func executeGUI(args []string, stdout, stderr io.Writer) int {
	var options guiOptions
	parser, err := kong.New(&options,
		kong.Name("ssh-keyselect-gui"),
		kong.Description("Start the SSH agent proxy and identity picker."),
		kong.Vars{"version": fmt.Sprintf("ssh-keyselect-gui %s (%s)", version, commit)},
		kong.Writers(stdout, stderr),
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: cannot configure argument parser: %v\n", guiCommandName, err)
		return 2
	}
	exitCode := -1
	parser.Exit = func(code int) { exitCode = code }
	if _, err := parser.Parse(args); err != nil {
		if exitCode == 0 {
			return 0
		}
		parser.Errorf("%s", err)
		return 2
	}
	if exitCode == 0 {
		return 0
	}
	configPath := ""
	if options.Config != nil {
		configPath = *options.Config
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	if options.Listen != nil {
		cfg.Agent.Listen = config.ExpandPath(*options.Listen)
	}
	if options.ListenMode != nil {
		mode, modeErr := transport.ParseMode(*options.ListenMode)
		if modeErr != nil {
			return cmdutil.ReportError(stderr, guiCommandName, modeErr)
		}
		cfg.Agent.ListenMode = mode
	}
	if options.Upstream != nil {
		cfg.Agent.Upstream = config.ExpandPath(*options.Upstream)
	}
	if options.UpstreamMode != nil {
		mode, modeErr := transport.ParseMode(*options.UpstreamMode)
		if modeErr != nil {
			return cmdutil.ReportError(stderr, guiCommandName, modeErr)
		}
		cfg.Agent.UpstreamMode = mode
	}
	if options.LogLevel != nil {
		cfg.Log.Level = *options.LogLevel
	}
	if err := cfg.Validate(); err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	requestedListenMode := cfg.Agent.ListenMode
	resolvedListenPath, effectiveListenMode, err := resolveGUIListen(cfg)
	if err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	activeConfigPath := configPath
	if activeConfigPath == "" {
		activeConfigPath, err = config.DefaultPath()
		if err != nil {
			return cmdutil.ReportError(stderr, guiCommandName, err)
		}
	}

	logger := cmdutil.NewLogger(stderr, cfg.Log.Level)
	endpointAgent := &guiEndpointAgent{}
	endpointAgent.Set(upstream.EndpointAgent{Path: cfg.Agent.Upstream, Mode: cfg.Agent.UpstreamMode})
	server := agentproxy.Server{
		Agent:  endpointAgent,
		Logger: logger,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	guiSelector := selector.NewGUISelector()
	guiSelector.SetRefreshCallback(func(refreshCtx context.Context) ([]identity.Identity, error) {
		endpoint, _ := endpointAgent.Snapshot()
		return endpoint.List(refreshCtx)
	})
	server.Selector = guiSelector
	logger.Info("SSH agent proxy started", "listen", resolvedListenPath, "ui", "gui")
	serveWithGUI(ctx, stop, cfg, activeConfigPath, resolvedListenPath, requestedListenMode, effectiveListenMode,
		endpointAgent, &server, guiSelector, logger, stderr)
	return 0
}

func guiMainWindowTitle(instanceSuffix string, dirty bool, statusSuffix string) string {
	title := branding.Name + instanceSuffix
	if dirty {
		title += " *"
	}
	return title + statusSuffix
}

func guiSamePath(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
	}
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	return firstErr == nil && secondErr == nil && filepath.Clean(firstAbs) == filepath.Clean(secondAbs)
}

func serveWithGUI(
	ctx context.Context,
	stop context.CancelFunc,
	cfg config.Config,
	configFilePath string,
	listenPath string,
	requestedListenMode, listenMode transport.Mode,
	endpointAgent *guiEndpointAgent,
	server *agentproxy.Server,
	guiSelector *selector.GUISelector,
	logger *slog.Logger,
	loggerOutput io.Writer,
) {
	go func() {
		<-ctx.Done()
		unison.InvokeTask(unison.AttemptQuit)
	}()

	var runtimeState *guiRuntime
	var configState *guiConfigState
	var cleanupTray func() error

	unison.Start(
		unison.StartupFinishedCallback(func() {
			configureGUIAppearance()
			window, err := unison.NewWindow(branding.Name)
			if err != nil {
				logger.Error("create GUI status window", "error", err)
				guiSelector.Stop()
				stop()
				unison.AttemptQuit()
				return
			}
			if icons, iconErr := guiassets.TitleIcons(); iconErr != nil {
				logger.Warn("create application icon", "error", iconErr)
			} else {
				window.SetTitleIcons(icons)
			}
			instanceTitleSuffix := ""
			statusTitleSuffix := ""
			updateWindowTitle := func() {
				window.SetTitle(guiMainWindowTitle(instanceTitleSuffix, configState != nil && configState.dirty, statusTitleSuffix))
			}
			window.AllowCloseCallback = func() bool {
				if configState == nil || !configState.dirty {
					return true
				}
				switch guiConfirmSaveBeforeClose(configState.filePath) {
				case unison.ModalResponseOK:
					if err := configState.save(); err != nil {
						showGUIErrorDialog("Could not save configuration.", err)
						return false
					}
					return true
				case unison.ModalResponseDiscard:
					return true
				case unison.ModalResponseCancel:
					return false
				default:
					return false
				}
			}

			content := window.Content()
			content.SetBorder(unison.NewEmptyBorder(geom.NewUniformInsets(guiStatusCardBorderInset + guiStatusCardPadding)))
			content.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 8})
			content.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
				canvas.DrawRect(rect, guiWindowInk.Paint(canvas, rect, paintstyle.Fill))
			}

			body := unison.NewPanel()
			body.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 7})
			body.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true, VGrab: true})

			identityCard := newGUIStatusCard()
			identityCard.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, VAlign: align.Fill, HGrab: true, VGrab: true})
			identityHeading := unison.NewPanel()
			identityHeading.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 12, VAlign: align.Middle})
			identityHeading.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			keyTitleGroup := unison.NewPanel()
			keyTitleGroup.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 8, VAlign: align.Middle})
			keyTitleGroup.AddChild(newGUISectionBadge("Keys", guiKeysBadgeFill, guiKeysBadgeInk))
			keysStatus := unison.NewLabel()
			keysStatus.SetTitle("Upstream not configured")
			keysStatus.Font = guiFont(9, false)
			keysStatus.OnBackgroundInk = guiMutedInk
			keysStatus.SetLayoutData(&unison.FlexLayoutData{
				VAlign: align.Middle, MinSize: geom.NewSize(200, 0), SizeHint: geom.NewSize(200, 0),
			})
			keyTitleGroup.AddChild(keysStatus)
			keyTitleGroup.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			identityHeading.AddChild(keyTitleGroup)

			refresh := guistyle.NewRefreshButton()
			identityHeading.AddChild(refresh)
			identityCard.AddChild(identityHeading)

			keyTable := guiidentitytable.New(false)
			selectedKeyRow := func() (*guiidentitytable.Row, bool) {
				selected := keyTable.Table.LeadRowIndex()
				rows := keyTable.Table.RootRows()
				if selected < 0 || selected >= len(rows) {
					return nil, false
				}
				return rows[selected], true
			}
			keyTable.Table.ContextMenuCallback = func(geom.Point) unison.Menu {
				row, ok := selectedKeyRow()
				if !ok {
					return nil
				}
				factory := unison.DefaultMenuFactory()
				menu := factory.NewMenu(unison.PopupMenuTemporaryBaseID|unison.ContextMenuIDFlag, "", nil)
				menu.InsertItem(-1, factory.NewItem(
					unison.PopupMenuTemporaryBaseID+1|unison.ContextMenuIDFlag,
					"Copy", unison.KeyBinding{}, nil,
					func(unison.MenuItem) { unison.ClipboardSetText(row.CopyText()) },
				))
				return menu
			}
			keyTable.Table.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
				if keyCode == unison.KeyC && modifiers.OSMenuCommandDown() {
					if row, ok := selectedKeyRow(); ok {
						unison.ClipboardSetText(row.CopyText())
						return true
					}
				}
				return keyTable.Table.DefaultKeyDown(keyCode, modifiers, repeat)
			}
			keysScroll := unison.NewScrollPanel()
			keyTable.AttachTo(keysScroll)
			keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(0, guiidentitytable.MaxVisibleRows))
			identityCard.AddChild(keysScroll)
			body.AddChild(identityCard)

			connectionCard := newGUIStatusCard()
			connectionCard.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			connectionCard.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 7})
			connectionTitle := unison.NewPanel()
			connectionTitle.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 8, VAlign: align.Middle})
			connectionTitle.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			connectionTitle.AddChild(newGUISectionBadge("Agent Proxy", guiActiveFillInk, guiActiveInk))
			autoSelectControls := unison.NewPanel()
			autoSelectControls.SetLayout(&unison.FlexLayout{Columns: 3, HSpacing: 4, VAlign: align.Middle})
			autoSelectControls.SetLayoutData(&unison.FlexLayoutData{HAlign: align.End, HGrab: true})
			autoSelectLabel := newGUISectionBadge("Auto Select", guiKeysBadgeFill, guiKeysBadgeInk)
			autoSelectLabel.Tooltip = unison.NewTooltipWithText(
				"When On, every upstream key is available without a per-connection selection.")
			autoSelectOff := unison.NewButton()
			autoSelectOff.SetTitle("Off")
			autoSelectOn := unison.NewButton()
			autoSelectOn.SetTitle("On")
			autoSelectTooltip := unison.NewTooltipWithText(
				"On exposes every upstream identity to clients using this proxy. Turn it Off after batch work.")
			autoSelectOff.Tooltip = autoSelectTooltip
			autoSelectOn.Tooltip = autoSelectTooltip
			updateAutoSelectButtons := func() {
				enabled := server.AutoSelect()
				styleGUIAutoSelectButton(autoSelectOff, !enabled, false)
				styleGUIAutoSelectButton(autoSelectOn, enabled, true)
			}
			autoSelectOff.ClickCallback = func() {
				server.SetAutoSelect(false)
				updateAutoSelectButtons()
			}
			autoSelectOn.ClickCallback = func() {
				if server.AutoSelect() || !guiConfirmAutoSelectEnable() {
					return
				}
				server.SetAutoSelect(true)
				updateAutoSelectButtons()
			}
			autoSelectControls.AddChild(autoSelectLabel)
			autoSelectControls.AddChild(autoSelectOff)
			autoSelectControls.AddChild(autoSelectOn)
			updateAutoSelectButtons()
			proxyTitleControls := unison.NewPanel()
			proxyTitleControls.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 10, VAlign: align.Middle})
			proxyTitleControls.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			settingsButton := unison.NewButton()
			settingsButton.Font = guiSymbolFont(13.5, true)
			styleGUIAccentButton(settingsButton)
			settingsButton.SetTitle("⚙")
			// The gear glyph sits slightly high in the platform symbol font.
			centerGUIButtonGlyph(settingsButton, 0, 1.5)
			// Compensate for the gear glyph's left-heavy visual bounds.
			settingsButton.HAlign = align.End
			settingsButton.DrawCallback = func(canvas *unison.Canvas, _ geom.Rect) {
				guistyle.DrawAccentButtonWithWhiteGlyph(settingsButton, canvas, 0.5)
			}
			settingsButton.Tooltip = unison.NewTooltipWithText("Settings")
			guistyle.SetEqualCompactIconButtonSizes(refresh, settingsButton)
			settingsButton.SetLayoutData(&unison.FlexLayoutData{HAlign: align.End, VAlign: align.Middle})
			proxyTitleControls.AddChild(settingsButton)
			proxyTitleControls.AddChild(autoSelectControls)
			connectionTitle.AddChild(proxyTitleControls)
			connectionCard.AddChild(connectionTitle)

			connectionRows := unison.NewPanel()
			connectionRows.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 5})
			connectionRows.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})
			updateUpstreamRow := guiAddConnectionModeRow(connectionRows, guiAccentInk, guiKeysBadgeFill, guiKeysBadgeInk,
				"Upstream", "UPSTREAM_SSH_AUTH_SOCK", "", "", transport.Auto, nil)
			separator := unison.NewSeparator()
			separator.LineInk = guiBorderInk
			connectionRows.AddChild(separator)
			updateListenRow := guiAddConnectionModeRow(connectionRows, guiActiveInk, guiActiveFillInk, guiActiveInk,
				"Listen", "SSH_AUTH_SOCK", "Proxy", listenPath, listenMode, nil)
			connectionCard.AddChild(connectionRows)
			body.AddChild(connectionCard)
			content.AddChild(body)

			firstIdentityRefresh := true
			finishIdentityLayout := func() {
				keysStatus.MarkForLayoutAndRedraw()
				if firstIdentityRefresh {
					firstIdentityRefresh = false
					window.Pack()
					guiwindow.CenterOnPrimaryDisplay(window)
					return
				}
				_, preferred, _ := content.Sizes(geom.Size{})
				current := window.ContentRect()
				if current.Size.Width < preferred.Width || current.Size.Height < preferred.Height {
					window.SetContentRect(geom.NewRect(
						current.Point.X, current.Point.Y,
						max(current.Size.Width, preferred.Width), max(current.Size.Height, preferred.Height),
					))
					window.EnsureOnDisplay()
				}
			}

			var refreshIdentities func()
			refreshIdentities = func() {
				endpoint, generation := endpointAgent.Snapshot()
				if endpoint.Path == "" {
					keyTable.SetRows(nil)
					keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(0, guiidentitytable.MaxVisibleRows))
					keysStatus.SetTitle("Upstream not configured")
					keysStatus.Tooltip = nil
					refresh.SetEnabled(false)
					finishIdentityLayout()
					return
				}
				refresh.SetEnabled(false)
				keysStatus.SetTitle("Loading keys...")
				keysStatus.Tooltip = nil
				keysStatus.MarkForLayoutAndRedraw()
				go func() {
					requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					identities, listErr := endpoint.List(requestCtx)
					unison.InvokeTask(func() {
						if !window.IsValid() {
							return
						}
						_, currentGeneration := endpointAgent.Snapshot()
						if currentGeneration != generation {
							return
						}
						refresh.SetEnabled(true)
						if listErr != nil {
							keyTable.SetRows(nil)
							keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(0, guiidentitytable.MaxVisibleRows))
							keysStatus.SetTitle("Could not load keys")
							keysStatus.Tooltip = unison.NewTooltipWithText(listErr.Error())
						} else if len(identities) == 0 {
							keyTable.SetRows(nil)
							keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(0, guiidentitytable.MaxVisibleRows))
							keysStatus.SetTitle("0 keys")
						} else {
							keyTable.SetRows(identities)
							keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(len(identities), guiidentitytable.MaxVisibleRows))
							countLabel := fmt.Sprintf("%d keys", len(identities))
							if len(identities) == 1 {
								countLabel = "1 key"
							}
							keysStatus.SetTitle(countLabel)
						}
						finishIdentityLayout()
					})
				}()
			}
			refresh.ClickCallback = refreshIdentities

			openSettings := func() {
				next, ok, editErr := editGUIEndpointSettings(configState.cfg)
				if editErr != nil {
					showGUIErrorDialog("Could not open settings.", editErr)
					return
				}
				if !ok {
					return
				}
				dirty := configState.dirty || next != configState.cfg
				if err := configState.apply(next, dirty); err != nil {
					showGUIErrorDialog("Could not apply settings.", err)
				}
			}
			settingsButton.ClickCallback = openSettings
			configState = &guiConfigState{
				cfg: cfg, filePath: configFilePath, actualListen: listenPath,
				effectiveListenMode: listenMode, onDirtyChange: updateWindowTitle,
			}
			runtimeState = &guiRuntime{
				ctx: ctx, server: server, agent: endpointAgent, loggerOutput: loggerOutput,
				listenPath: listenPath, listenMode: listenMode, currentLogLevel: cfg.Log.Level,
			}
			runtimeState.onServeError = func(serveErr error) {
				if !window.IsValid() {
					return
				}
				keysStatus.SetTitle("Agent proxy stopped")
				keysStatus.Tooltip = unison.NewTooltipWithText(serveErr.Error())
				finishIdentityLayout()
			}
			configState.runtime = runtimeState
			updateConnectionRows := func() {
				endpoint, _ := endpointAgent.Snapshot()
				var effectiveUpstreamMode transport.Mode
				var modeErr error
				if endpoint.Path != "" {
					effectiveUpstreamMode, modeErr = upstream.ResolveMode(endpoint.Path, endpoint.Mode)
				}
				updateUpstreamRow(endpoint.Path, effectiveUpstreamMode, modeErr)
				updateListenRow(configState.actualListen, configState.effectiveListenMode, nil)
			}
			configState.onUpdate = func() {
				updateConnectionRows()
				keyTable.SetRows(nil)
				keysScroll.SetLayoutData(guiidentitytable.ScrollLayoutData(0, guiidentitytable.MaxVisibleRows))
				refreshIdentities()
			}
			updateConnectionRows()

			installGUIFileMenu(window, guiFileMenuActions{
				open: configState.open, save: configState.saveFromMenu, saveAs: configState.saveAs, settings: openSettings,
			})
			window.SetContentRect(geom.NewRect(100, 100, 1080, 510))
			guiwindow.CenterOnPrimaryDisplay(window)
			window.ToFront()

			startTray := func() (func() error, error) {
				if !systemtray.Supported() {
					return nil, nil
				}
				trayIconPNG, iconErr := guiassets.PNG(16)
				if iconErr != nil {
					return nil, fmt.Errorf("decode system tray icon: %w", iconErr)
				}
				showWindow := func() {
					unison.InvokeTask(func() {
						if window.IsValid() {
							if window.IsMinimized() {
								window.Minimize()
							}
							window.Show()
							window.ToFront()
						}
					})
				}
				trayCleanup, titleSuffix, trayErr := systemtray.Start(systemtray.Callbacks{
					Show:    showWindow,
					Quit:    func() { unison.InvokeTask(unison.AttemptQuit) },
					Refresh: func() { unison.InvokeTask(refreshIdentities) },
					IconPNG: trayIconPNG,
				})
				if trayErr == nil {
					instanceTitleSuffix = titleSuffix
					updateWindowTitle()
					window.MinimizedCallback = func(minimized bool) {
						if minimized {
							window.Hide()
						}
					}
				}
				return trayCleanup, trayErr
			}

			// Register the icon even when another instance already owns the proxy endpoint.
			var trayErr, proxyErr error
			cleanupTray, trayErr, proxyErr = startGUIComponents(startTray, func() error {
				_, _, applyErr := configState.runtime.Apply(cfg)
				return applyErr
			})
			if trayErr != nil {
				logger.Error("create system tray icon", "error", trayErr)
				statusTitleSuffix = " - tray icon unavailable"
				updateWindowTitle()
				keysStatus.SetTitle("Could not create system tray icon")
				keysStatus.Tooltip = unison.NewTooltipWithText(trayErr.Error())
				keysStatus.MarkForLayoutAndRedraw()
			}
			if proxyErr != nil {
				logger.Error("listen on agent endpoint", "error", proxyErr)
				keysStatus.SetTitle("Agent proxy could not start")
				keysStatus.Tooltip = unison.NewTooltipWithText(proxyErr.Error())
				refresh.SetEnabled(false)
				finishIdentityLayout()
				return
			}
			configState.actualListen = runtimeState.listenPath
			configState.effectiveListenMode = runtimeState.listenMode
			configState.onUpdate()
		}),
		unison.QuittingCallback(func() {
			guiSelector.Stop()
			stop()
			if runtimeState != nil {
				runtimeState.Close()
			}
			if cleanupTray != nil {
				if err := cleanupTray(); err != nil {
					logger.Error("remove system tray icon", "error", err)
				}
			}
		}),
	)
}
