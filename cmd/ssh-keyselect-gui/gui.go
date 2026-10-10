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
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/alecthomas/kong"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/guitable"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

// guiCommandName identifies the executable in help and startup errors.
const guiCommandName = "ssh-keyselect-gui"

// guiApp owns the UI state; MyGo builds each native frame from this state.
type guiApp struct {
	ctx          context.Context
	stop         context.CancelFunc
	logger       *slog.Logger
	loggerOutput io.Writer
	cfg          config.Config
	configPath   string
	dirty        bool
	applying     bool
	actualListen string
	listenMode   transport.Mode

	endpointAgent  *guiEndpointAgent
	server         *agentproxy.Server
	selector       *selector.GUISelector
	runtime        *guiRuntime
	window         *mygo.Window
	mainWindow     atomic.Pointer[mygo.Window]
	mainVisible    atomic.Bool
	tray           *mygo.Tray
	trayAutoSelect *mygo.MenuItem

	identities        []identity.Identity
	identityTable     ui.ListState
	identitySort      ui.SortOrder
	identityRowsCache []guitable.IdentityRow
	identityRowsSort  ui.SortOrder
	identityRowsValid bool
	selectedIdentity  int
	refreshGeneration uint64
	keyStatus         string
	keyError          string
	loading           bool
	statusMessage     string
	autoSelect        bool
	confirmAutoSelect bool
	closePrompt       bool
	closeConfirmed    bool
	fileMenuOpen      bool
	helpMenuOpen      bool
	aboutWindow       *mygo.Window
	uiStarted         bool

	shutdownOnce sync.Once
}

type guiOptions struct {
	Config       *string          `help:"TOML configuration file." placeholder:"FILE"`
	Listen       *string          `help:"Frontend endpoint (default: HOME socket, or the fixed Windows OpenSSH named-pipe endpoint)." placeholder:"ENDPOINT"`
	ListenMode   *string          `help:"Frontend protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	Upstream     *string          `help:"Upstream endpoint (default: UPSTREAM_SSH_AUTH_SOCK, then SSH_AUTH_SOCK)." placeholder:"ENDPOINT"`
	UpstreamMode *string          `help:"Upstream protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	LogLevel     *string          `help:"Log level (off, debug, info, warn, error)." placeholder:"LEVEL"`
	Version      kong.VersionFlag `help:"Print version information and quit."`
}

// executeGUI parses options, starts the native UI event loop, and owns the proxy lifecycle.
func executeGUI(args []string, stdout, stderr io.Writer) int {
	var options guiOptions
	parser, err := kong.New(&options,
		kong.Name(guiCommandName),
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

	listenPath, effectiveListenMode, err := resolveGUIListen(cfg)
	if err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	listenPathExists, err := guiListenPathExists(listenPath)
	if err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	if listenPathExists {
		listenPath = ""
	}
	if configPath == "" {
		configPath, err = config.DefaultPath()
		if err != nil {
			return cmdutil.ReportError(stderr, guiCommandName, err)
		}
	}

	logOutput, err := newGUILogOutput(stderr, cfg.Log.File)
	if err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	var logLevel slog.LevelVar
	logger := newGUILogger(logOutput, &logLevel, cfg.Log.Level)
	slog.SetDefault(logger)
	ctx, stop := context.WithCancel(context.Background())
	endpointAgent := &guiEndpointAgent{}
	endpointAgent.Set(upstream.EndpointAgent{Path: cfg.Agent.Upstream, Mode: cfg.Agent.UpstreamMode})
	guiSelector := selector.NewGUISelector()
	guiSelector.SetRefreshCallback(func(refreshCtx context.Context) ([]identity.Identity, error) {
		endpoint, _ := endpointAgent.Snapshot()
		return endpoint.List(refreshCtx)
	})
	server := &agentproxy.Server{Agent: endpointAgent, Selector: guiSelector, Logger: logger}
	app := &guiApp{
		ctx: ctx, stop: stop, logger: logger, loggerOutput: logOutput, cfg: cfg,
		configPath: configPath, actualListen: listenPath, listenMode: effectiveListenMode,
		endpointAgent: endpointAgent, server: server, selector: guiSelector,
		selectedIdentity: -1,
	}
	guiSelector.SetWindowProvider(func() *mygo.Window {
		window := app.mainWindow.Load()
		if window != nil && !window.IsDestroyed() && app.mainVisible.Load() {
			return window
		}
		return nil
	})
	app.runtime = &guiRuntime{
		ctx: ctx, server: server, agent: endpointAgent, loggerOutput: logOutput, logOutput: logOutput, logLevel: &logLevel,
		listenPath: listenPath, listenMode: effectiveListenMode, currentLogLevel: cfg.Log.Level,
		currentLogFile: cfg.Log.File,
	}
	app.runtime.onServeError = func(serveErr error) {
		if window := app.mainWindow.Load(); window != nil {
			window.Update(func() {
				app.actualListen = ""
				app.statusMessage = "Agent proxy stopped: " + serveErr.Error()
				app.updateWindowTitle()
				if app.window != nil {
					app.window.Invalidate()
				}
			})
		}
	}

	mygo.App.SetName("SSH KeySelect")
	mygo.App.SetVersion(version)
	if err := setGUIUserDataPath(); err != nil {
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	mygo.Theme.SetSource(mygo.ThemeSource(cfg.GUI.Theme))
	// Keep the native application menu on macOS; other platforms use the
	// in-window menu bar so its text follows the app's normal UI size.
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(app.applicationMenu())
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.shutdown() })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && app.uiStarted {
			app.showMainWindow()
		}
	})
	mygo.App.WhenReady(app.start)
	if err := mygo.App.Run(); err != nil {
		app.shutdown()
		return cmdutil.ReportError(stderr, guiCommandName, err)
	}
	app.shutdown()
	return 0
}

// setGUIUserDataPath keeps MyGo's per-user files alongside ssh-keyselect config.
func setGUIUserDataPath() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locate user data directory: %w", err)
	}
	path := filepath.Join(base, "ssh-keyselect")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create user data directory: %w", err)
	}
	mygo.App.SetPath(mygo.PathUserData, path)
	return nil
}

func (a *guiApp) shutdown() {
	a.shutdownOnce.Do(func() {
		a.selector.Stop()
		a.stop()
		if a.runtime != nil {
			a.runtime.Close()
		}
		if a.tray != nil {
			a.tray.Destroy()
		}
		if output, ok := a.loggerOutput.(io.Closer); ok {
			_ = output.Close()
		}
	})
}
