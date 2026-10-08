// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jfut/ssh-keyselect/internal/agentproxy"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/cmdutil"
	"github.com/jfut/ssh-keyselect/internal/config"
	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

var (
	version = "dev"
	commit  = "none"
)

const commandName = "ssh-keyselect"
const upstreamRequiredMessage = "upstream agent is required; use --upstream or set UPSTREAM_SSH_AUTH_SOCK or SSH_AUTH_SOCK"

type cliOptions struct {
	SSH     sshCommandOptions     `cmd:"" help:"Run OpenSSH with per-connection key selection from an existing SSH agent."`
	List    listCommandOptions    `cmd:"" help:"List available keys from the upstream SSH agent."`
	Test    listCommandOptions    `cmd:"" help:"Check the upstream SSH agent."`
	Version versionCommandOptions `cmd:"" help:"Print build version and commit."`
}

type sshCommandOptions struct {
	Upstream         *string  `help:"Upstream SSH agent endpoint." placeholder:"ENDPOINT"`
	UpstreamMode     *string  `help:"Upstream protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	Listen           *string  `help:"Proxy endpoint for OpenSSH." placeholder:"ENDPOINT"`
	ListenMode       *string  `help:"Proxy protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
	LogLevel         *string  `help:"Log level (off, debug, info, warn, error)." placeholder:"LEVEL"`
	LogFile          *string  `help:"Write ssh-keyselect diagnostic logs to a file, replacing it for this run." placeholder:"FILE"`
	SelectionTimeout *int     `help:"Seconds to wait for a key selection (default: 120, range: 1 to 86399)." placeholder:"SECONDS"`
	SSHArgs          []string `arg:"" optional:"" passthrough:"all" name:"SSH arguments" help:"Arguments passed to OpenSSH."`
}

type listCommandOptions struct {
	Upstream     *string `help:"Upstream SSH agent socket or named-pipe endpoint." placeholder:"ENDPOINT"`
	UpstreamMode *string `help:"Upstream protocol mode (auto, cygwin, unix, named-pipe, wsl1)." placeholder:"MODE"`
}

type versionCommandOptions struct{}

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }

func execute(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-V") {
		_, _ = fmt.Fprintf(stdout, "%s %s (%s)\n", commandName, version, commit)
		return 0
	}
	if len(args) == 0 || args[0] == "help" {
		args = []string{"--help"}
	}

	var options cliOptions
	parser, err := kong.New(&options,
		kong.Name(commandName),
		kong.Description(branding.Description+"\n\nProject URL: "+branding.ProjectURL+"\nAuthor: "+branding.Author),
		// Keep the app description and metadata in predictable lines for terminal help.
		kong.HelpOptions{WrapUpperBound: 79, NoAppDescFormat: true},
		kong.Writers(stdout, stderr),
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: cannot configure argument parser: %v\n", commandName, err)
		return 2
	}
	exitCode := -1
	parser.Exit = func(code int) { exitCode = code }
	ctx, err := parser.Parse(args)
	if err != nil {
		if exitCode == 0 {
			return 0
		}
		parser.Errorf("%s", err)
		return 2
	}
	if exitCode == 0 {
		return 0
	}

	command := ""
	if selected := ctx.Selected(); selected != nil {
		command = selected.Name
	}
	switch command {
	case "ssh":
		return executeSSH(options.SSH, stdout, stderr)
	case "list":
		return executeListOptions(options.List, stdout, stderr, false)
	case "test":
		return executeListOptions(options.Test, stdout, stderr, true)
	case "version":
		_, _ = fmt.Fprintf(stdout, "%s %s (%s)\n", commandName, version, commit)
		return 0
	default:
		return 0
	}
}

// executeSSH runs OpenSSH through a short-lived TUI proxy attached to this terminal.
func executeSSH(options sshCommandOptions, stdout, stderr io.Writer) (exitCode int) {
	sshArgs := options.SSHArgs
	if len(sshArgs) > 0 && sshArgs[0] == "--" {
		sshArgs = sshArgs[1:]
	}
	cfg := cliConfig()
	if options.Upstream != nil {
		if *options.Upstream == "" {
			_, _ = fmt.Fprintf(stderr, "%s: --upstream requires a value\n", commandName)
			return 2
		}
		cfg.Agent.Upstream = config.ExpandPath(*options.Upstream)
	}
	if options.UpstreamMode != nil {
		if *options.UpstreamMode == "" {
			_, _ = fmt.Fprintf(stderr, "%s: --upstream-mode requires a value\n", commandName)
			return 2
		}
		mode, err := transport.ParseMode(*options.UpstreamMode)
		if err != nil {
			return cmdutil.ReportError(stderr, commandName, err)
		}
		cfg.Agent.UpstreamMode = mode
	}
	if options.ListenMode != nil {
		if *options.ListenMode == "" {
			_, _ = fmt.Fprintf(stderr, "%s: --listen-mode requires a value\n", commandName)
			return 2
		}
		mode, err := transport.ParseMode(*options.ListenMode)
		if err != nil {
			return cmdutil.ReportError(stderr, commandName, err)
		}
		cfg.Agent.ListenMode = mode
	}
	if options.LogLevel != nil {
		if *options.LogLevel == "" {
			_, _ = fmt.Fprintf(stderr, "%s: --log-level requires a value\n", commandName)
			return 2
		}
		cfg.Log.Level = *options.LogLevel
	}
	if options.SelectionTimeout != nil {
		cfg.Agent.SelectionTimeout = *options.SelectionTimeout
	}
	if options.LogFile != nil && *options.LogFile == "" {
		_, _ = fmt.Fprintf(stderr, "%s: --log-file requires a value\n", commandName)
		return 2
	}
	var err error
	if err := cfg.Validate(); err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	if cfg.Agent.Upstream == "" {
		return cmdutil.ReportError(stderr, commandName, errors.New(upstreamRequiredMessage))
	}
	var logFile *os.File
	if options.LogFile != nil {
		logFile, err = openLogFile(*options.LogFile)
		if err != nil {
			return cmdutil.ReportError(stderr, commandName, fmt.Errorf("open log file: %w", err))
		}
		defer func() {
			if closeErr := logFile.Close(); closeErr != nil {
				closeCode := cmdutil.ReportError(stderr, commandName, fmt.Errorf("close log file: %w", closeErr))
				if exitCode == 0 {
					exitCode = closeCode
				}
			}
		}()
	}

	listenPath := ""
	if options.Listen != nil {
		if *options.Listen == "" {
			_, _ = fmt.Fprintf(stderr, "%s: --listen requires a value\n", commandName)
			return 2
		}
		listenPath = *options.Listen
	}
	if listenPath == "" {
		listenPath, cfg.Agent.ListenMode, err = listener.DefaultTUIEndpointForMode(cfg.Agent.Upstream, cfg.Agent.ListenMode)
		if err != nil {
			return cmdutil.ReportError(stderr, commandName, err)
		}
	} else {
		listenPath = config.ExpandPath(listenPath)
		cfg.Agent.ListenMode, err = listener.ResolveMode(listenPath, cfg.Agent.Upstream, cfg.Agent.ListenMode)
		if err != nil {
			return cmdutil.ReportError(stderr, commandName, err)
		}
	}
	// Reject self-connections before binding can create or replace the upstream endpoint.
	if transport.SameEndpoint(listenPath, cfg.Agent.Upstream) {
		return cmdutil.ReportError(stderr, commandName, errors.New("listen and upstream endpoints must be different"))
	}
	ln, cleanup, err := listener.ListenWithMode(listenPath, cfg.Agent.ListenMode)
	if err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	defer cleanup()

	logOutput := stderr
	if logFile != nil {
		logOutput = logFile
	}
	logger := cmdutil.NewLogger(logOutput, cfg.Log.Level)
	tuiSelector := selector.NewTUISelector()
	server := agentproxy.Server{
		Agent:    upstream.EndpointAgent{Path: cfg.Agent.Upstream, Mode: cfg.Agent.UpstreamMode},
		Selector: tuiSelector,
		Logger:   logger,
	}
	server.SetSelectionTimeout(time.Duration(cfg.Agent.SelectionTimeout) * time.Second)
	ctx, cancelProxy := context.WithCancel(context.Background())
	defer cancelProxy()
	commandCtx, cancelCommand := context.WithCancel(ctx)
	defer cancelCommand()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	var sshRunning atomic.Bool
	interruptDone := make(chan struct{})
	go func() {
		defer close(interruptDone)
		for {
			select {
			case <-ctx.Done():
				return
			case received := <-interrupts:
				// Do not cancel OpenSSH for Ctrl+C while it is running. The picker handles
				// Ctrl+C as a selection cancel; outside the picker, OpenSSH receives it.
				if received == os.Interrupt && sshRunning.Load() {
					continue
				}
				cancelCommand()
				cancelProxy()
			}
		}
	}()
	defer func() {
		signal.Stop(interrupts)
		cancelProxy()
		cancelCommand()
		<-interruptDone
	}()
	serverDone := make(chan error, 1)
	go func() {
		err := server.Serve(ctx, ln)
		serverDone <- err
		if err != nil && ctx.Err() == nil {
			cancelCommand()
		}
	}()

	logger.Info("SSH agent proxy started", "listen", listenPath, "ui", "tui")
	command := newSSHCommand(commandCtx, sshArgs, listenPath, stdout, stderr)
	sshRunning.Store(true)
	runErr := command.Run()
	sshRunning.Store(false)
	cancelProxy()
	if serveErr := <-serverDone; serveErr != nil {
		return cmdutil.ReportError(stderr, commandName, fmt.Errorf("SSH agent proxy stopped: %w", serveErr))
	}
	logger.Info("SSH agent proxy stopped")
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return sshExitCode(exitErr)
		}
		return cmdutil.ReportError(stderr, commandName, fmt.Errorf("run ssh: %w", runErr))
	}
	return 0
}

// openLogFile starts a fresh log for the current SSH proxy run.
func openLogFile(path string) (*os.File, error) {
	file, err := os.OpenFile(config.ExpandPath(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// newSSHCommand gives only the child SSH process the temporary agent endpoint.
func newSSHCommand(ctx context.Context, args []string, listenPath string, stdout, stderr io.Writer) *exec.Cmd {
	command := exec.CommandContext(ctx, "ssh", args...)
	command.Env = cmdutil.WithEnvironment(os.Environ(), "SSH_AUTH_SOCK", listenPath)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command
}

func executeListOptions(options listCommandOptions, stdout, stderr io.Writer, testMode bool) int {
	cfg := cliConfig()
	var err error
	if options.Upstream != nil {
		cfg.Agent.Upstream = config.ExpandPath(*options.Upstream)
	}
	if options.UpstreamMode != nil {
		cfg.Agent.UpstreamMode, err = transport.ParseMode(*options.UpstreamMode)
	}
	if err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	if cfg.Agent.Upstream == "" {
		return cmdutil.ReportError(stderr, commandName, errors.New(upstreamRequiredMessage))
	}
	identities, err := (upstream.EndpointAgent{Path: cfg.Agent.Upstream, Mode: cfg.Agent.UpstreamMode}).List(context.Background())
	if err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	if testMode {
		return printAgentTest(stdout, stderr, identities)
	}
	return printIdentities(stdout, stderr, identities)
}

// cliConfig resolves CLI defaults from built-in values and environment variables without reading TOML files.
func cliConfig() config.Config {
	cfg := config.Default()
	cfg.Agent.Upstream = config.ExpandPath(config.UpstreamFromEnvironment())
	return cfg
}

func printIdentities(w, stderr io.Writer, identities []identity.Identity) int {
	if _, err := io.WriteString(w, selector.FormatIdentityTable(identities)); err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	return 0
}

func printAgentTest(w, stderr io.Writer, identities []identity.Identity) int {
	counts := make(map[string]int)
	for _, id := range identities {
		counts[id.Algorithm]++
	}
	algorithms := make([]string, 0, len(counts))
	for algorithm := range counts {
		algorithms = append(algorithms, algorithm)
	}
	sort.Strings(algorithms)
	if _, err := fmt.Fprintf(w, "Upstream agent: OK\nIdentities: %d\nAlgorithms:\n", len(identities)); err != nil {
		return cmdutil.ReportError(stderr, commandName, err)
	}
	for _, algorithm := range algorithms {
		if _, err := fmt.Fprintf(w, "  %s: %d\n", algorithm, counts[algorithm]); err != nil {
			return cmdutil.ReportError(stderr, commandName, err)
		}
	}
	return 0
}
