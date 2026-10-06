# Developer notes

`README.md` is the source of truth for user-visible CLI, output, configuration, and security guidance. This document covers package boundaries, implementation details including internal safeguards and resource limits, development commands, and release procedures.

## Architecture

- `cmd/ssh-keyselect` implements the CLI and terminal SSH wrapper.
- `cmd/ssh-keyselect-gui` implements the MyGo native UI, app lifecycle, endpoint configuration, status display, and About surface. It is built with the `gui` build tag and shipped beside the CLI executable.
- `internal/protocol` encodes and parses SSH Agent Protocol messages. `internal/agentproxy` exposes the selected identity and forwards permitted signing requests to the upstream agent.
- `internal/listener` and `internal/upstream` implement frontend and upstream transports. `internal/transport` provides their shared mode names and endpoint comparisons.
- `internal/selector` contains terminal and MyGo native identity pickers. Its selection interface always receives a `SelectionContext`, including an empty context when no host key was verified.
- `internal/guitable` shares identity table columns and alternating row styling between the main window and GUI picker.
- `internal/config` reads and writes GUI TOML configuration. CLI `ssh`, `list`, and `test` commands resolve upstream defaults from flags and environment without loading that file.
- `internal/credits` embeds the compact dependency list used by About. Full license texts are shipped in the root `CREDITS` file.

### Agent proxy request flow

The SSH client may provide session bindings before requesting identities, then add bindings as forwarding hops are established. After the user selects an identity, the client sends a separate signing request when authentication needs a signature. For each bound operation, the proxy opens an upstream agent connection, replays the verified binding chain on that connection, and sends the identity-list, binding, or signing request. It closes the connection after the response, so no bound upstream connection sits idle while the picker is open. When a later forwarding hop follows an authentication binding, the proxy starts a new binding chain and asks for a fresh selection.

```mermaid
sequenceDiagram
    participant SSH as SSH client
    participant Proxy as ssh-keyselect listen socket
    participant Picker as TUI or GUI picker
    participant Agent as Upstream agent

    opt OpenSSH sends session bindings
        loop Each session binding
            SSH->>Proxy: ExtensionRequest session-bind@openssh.com
            Proxy->>Proxy: Verify host-key signature
            Proxy->>Agent: Open temporary upstream connection
            loop Replay previously accepted bindings
                Proxy->>Agent: Forward session-bind
                Agent-->>Proxy: SSH_AGENT_SUCCESS
            end
            Proxy->>Agent: Forward the new session-bind
            alt Upstream agent accepts binding
                Agent-->>Proxy: SSH_AGENT_SUCCESS
            else Upstream agent rejects binding
                Agent-->>Proxy: SSH_AGENT_FAILURE
            end
            Proxy->>Agent: Close temporary connection
            Proxy-->>SSH: Return the binding result
        end
    end

    SSH->>Proxy: RequestIdentities
    alt A session binding was received
        Proxy->>Agent: Open temporary upstream connection
        loop Replay verified binding chain
            Proxy->>Agent: Forward session-bind
            Agent-->>Proxy: SSH_AGENT_SUCCESS
        end
        Proxy->>Agent: Request identities
        Agent-->>Proxy: IdentitiesAnswer with available keys
        Proxy->>Agent: Close temporary connection
    else No session binding was received
        Proxy->>Agent: Request identities through a temporary connection
        Agent-->>Proxy: IdentitiesAnswer with available keys
    end
    alt Auto Select is enabled
        Proxy->>Proxy: Return all identities
    else Interactive selection
        Proxy->>Picker: Show available keys and wait for selection
        Note over Picker: The picker shows its display time and verified host-key details when available
        Note over Proxy,Agent: No upstream connection remains open while waiting
        Picker-->>Proxy: Selected identities, or cancel
    end
    Proxy->>Proxy: Encode and cache the identity answer and selected key digests for this binding chain
    Proxy-->>SSH: IdentitiesAnswer with selected keys, or an empty list on cancel

    opt Client repeats RequestIdentities on this socket connection
        SSH->>Proxy: RequestIdentities
        Proxy-->>SSH: Cached IdentitiesAnswer, without opening another picker
    end

    opt A later forwarding hop follows an authentication binding
        SSH->>Proxy: ExtensionRequest session-bind@openssh.com
        Proxy->>Proxy: Verify host-key signature
        Proxy->>Proxy: Clear the previous binding chain and selection
        Proxy->>Agent: Open a temporary connection, replay the chain, and forward the new binding
        Agent-->>Proxy: SSH_AGENT_SUCCESS
        Proxy->>Agent: Close temporary connection
        Proxy-->>SSH: Return the binding result
        SSH->>Proxy: RequestIdentities
        Proxy->>Agent: Open a temporary connection, replay the new chain, and request identities
        Agent-->>Proxy: IdentitiesAnswer with available keys
        Proxy->>Agent: Close temporary connection
        Proxy->>Picker: Ask for a fresh identity selection
        Picker-->>Proxy: Selected identity
        Proxy-->>SSH: IdentitiesAnswer with the selected key
    end

    loop Each later signing request
        SSH->>Proxy: SignRequest with key blob and data
        alt Key digest is in the selected set
            Proxy->>Agent: Open temporary connection and replay the binding chain
            Proxy->>Agent: Send SignRequest
            Agent-->>Proxy: SignResponse or SSH_AGENT_FAILURE
            Proxy->>Agent: Close temporary connection
            Proxy-->>SSH: Return the signature response
        else Key was not selected
            Proxy-->>SSH: SSH_AGENT_FAILURE
        end
    end

    Note over Proxy: Selection resets for a new forwarding chain and is discarded when the socket connection closes.
```

OpenSSH records session bindings for the lifetime of an agent connection and rejects a later binding on a connection already bound for authentication ([OpenSSH agent protocol](https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.agent)). The proxy replays the binding chain on each temporary upstream connection before sending that operation. This preserves the session context for OpenSSH-compatible agents while accommodating agents that close connections after a short idle period. Without session bindings, ordinary requests use the upstream agent directly.

The proxy serializes each selected identity list once and retains that response alongside the authorized key digests until the binding chain changes or the listen-socket connection closes. Repeated identity requests reuse those bytes. Later signing requests for a selected key in the same chain do not open another picker; requests for unselected keys are rejected. The repeated `RequestIdentities` branch covers clients that query again on the same agent socket. OpenSSH's usual authentication flow fetches the list while preparing public-key authentication; it does not poll periodically while an SSH session is idle ([OpenSSH source](https://github.com/openssh/openssh-portable/blob/master/sshconnect2.c#L1543-L1571)).

Every upstream agent implementation supports opening a session. Identity-list and binding operations have a 10-second deadline, including connection establishment and binding replay. Signing also bounds connection establishment and binding replay, but waits for the signature using the client connection's context so upstream confirmation dialogs and hardware-key touch prompts can remain open longer than 10 seconds. Client disconnection or cancellation closes the upstream connection and interrupts that wait. Time spent choosing a key is also excluded from upstream request deadlines.

### Display text sanitization

Upstream identity parsing validates the complete public-key blob with `ssh.ParsePublicKey`, including the algorithm, key fields, and trailing data, before deriving display metadata. Unsupported or malformed keys reject the identity response. Parsing errors do not echo untrusted algorithm text.

`internal/identity.DisplayText` replaces invalid UTF-8 with U+FFFD and maps Unicode control characters and characters with the [Bidi_Control property](https://www.unicode.org/Public/UCD/latest/ucd/PropList.txt) to ASCII spaces. This prevents public key metadata and local host hints from injecting terminal controls or explicit text-direction changes into surrounding UI text. Ordinary multilingual text, combining marks, and joining characters retain their spelling and glyph shaping.

CLI identity tables, terminal picker rows, native GUI key tables and their clipboard text, and both pickers' `known_hosts` hints use this sanitizer for comments, algorithms, and fingerprints. Sanitization applies to display strings; the identity retains the original agent comment for protocol responses. CLI and TUI tables cap comment columns at 36 characters, algorithm columns at 64, and fingerprint columns at 50. Truncation scans only to the column boundary and appends an ellipsis, preventing a single long comment from expanding every row's padding.

GUI endpoint export commands use POSIX single-quote escaping, including embedded apostrophes. Paths containing command substitutions, variable references, backticks, or backslashes remain literal when pasted into a POSIX shell.

### Terminal input lifecycle

The terminal picker serializes prompts and interrupts and joins each pending byte or line read before releasing its selection slot. This also applies to the speculative read used to distinguish Escape from a cursor-key sequence. A standalone Escape stops input while keeping output and terminal modes available for the final newline; full session cleanup follows before the selection slot is released. Unix terminals use nonblocking file descriptors and expire the read deadline to interrupt input through Go's poller without closing the shared output descriptor.

Windows input reads run on a dedicated OS thread. Closing the session marks it stopped, cancels synchronous reads with `CancelSynchronousIo` and overlapped reads with `CancelIoEx`, and waits for the reader to exit before restoring console modes. Cancellation retries cover the interval immediately before a read starts. The worker's thread handle is protected until shutdown finishes, preventing cancellation from affecting a reused runtime thread. Git Bash/Cygwin inherited stdin remains open for later prompts and the SSH process.

### Frontend resource limits

Each proxy listener allows up to 128 concurrent client connections; additional connections are closed immediately. Admission counts each connection until both its request handler and frame reader have stopped.

Signing requests from separate client connections run concurrently through independent upstream connections. One client's approval wait does not block another client's signing request. Requests and responses within a client connection remain ordered.

Once a client starts sending an agent frame, it must finish within 10 seconds or the connection is closed. The read deadline starts after the first frame byte, is not extended by partial reads, and is cleared after a complete frame. It does not limit idle connections or time spent choosing a key.

Clients should wait for each response before sending another request. The frame reader runs separately so a disconnect or an incomplete-frame timeout can cancel an open picker. It queues at most one complete request and closes the client if another arrives while the queue is full, so delivery to the handler never blocks client monitoring.

Every reader exit cancels the connection context and closes the frontend socket to stop the picker and unblock response writes. Shutdown cancels pickers, closes frontend sockets, and waits for handlers and readers even when the listener fails.

Each frontend connection accepts at most 16 session-binding requests and retains no more than 1 MiB of verified binding messages for its current chain. Starting a new forwarding chain clears the previous chain. The display-only `known_hosts` lookup reads at most 1 MiB from each file and stops scanning a file if a line exceeds 64 KiB.

Unix listener cleanup unlinks only the socket it created, after checking filesystem identity. Automatic unlinking on listener close is disabled so a replacement at the same path remains untouched.

Unix TUI endpoints use `XDG_RUNTIME_DIR/ssh-keyselect` when configured, or `os.TempDir()/ssh-keyselect-<effective UID>` otherwise. The XDG directory and the private directory must belong to the effective UID. The private directory is opened without following a symlink; ownership is checked before permissions are set to `0700` through the directory descriptor. This separates users in a shared temporary directory and avoids changing another user's directory permissions.

### Windows endpoint implementation

The `cygwin` transport reads the socket file's endpoint information and connects through loopback TCP using the file's GUID handshake. The `wsl1` transport uses Windows AF_UNIX sockets with paths translated from `/mnt/<drive>/...`. The `named-pipe` mode uses Windows OpenSSH named pipes. Native Windows and shell-specific defaults are implemented in platform-suffixed files under `internal/listener`, `internal/upstream`, `internal/winpath`, and `cmd/ssh-keyselect-gui`.

Cygwin socket metadata reads are capped at 256 bytes. The listener immediately returns a connection whose handshake runs independently in the proxy's per-connection handler, after admission and before normal client logging or agent request handling. Connection reads and writes also enforce authentication before passing any agent traffic. Unauthenticated connections count against the proxy's 128-client limit, and their rejection does not generate client or overload logs. The transport also caps pending handshakes at 128 and closes excess connections. Each pending handshake expires 10 seconds after admission, even if connection I/O has not started. Authentication stops that timer without imposing a deadline on later signing approval waits. Dial cancellation interrupts the handshake, and listener shutdown closes every pending handshake immediately.

The CLI and GUI use the same endpoint comparison before binding. On Windows, native paths, Git Bash drive mounts, Cygwin `/cygdrive/` paths, and WSL1 drive mounts resolve through `internal/winpath`; named-pipe names and native paths compare without case sensitivity.

The GUI stores and displays paths in platform-specific formats. On Windows, dialog paths are converted to or from the selected transport when opening or saving configuration. A Listen mode change closes and reopens the listener, attempting to restore the previous listener if rebinding fails.

Configuration application runs on a worker that serializes listener shutdown and restart. Completion updates configuration and MyGo UI state on the UI thread only after successful application. Configuration actions are disabled while application is pending. The event loop stays available to finish cancelled picker tasks while server handlers drain. MyGo's `OnWillQuit` handler stops the selector first, cancels the application context, and waits for server shutdown and owned socket cleanup before returning. Closing the native picker releases its waiting agent request so shutdown can finish while the UI loop is active.

At GUI startup, an existing filesystem entry at the resolved Listen path leaves the proxy unconfigured. The preflight check prevents the listener from removing or replacing an existing socket file.

### GUI rendering and memory

The GUI uses MyGo's Go-only `ui` package. Views rebuild from application state, and its virtualized tables create visible rows on demand. The main identity table reuses its sorted rows between state changes, and the picker reuses its searchable identities and matches between query changes. Native windows do not start a WebView or load HTML or JavaScript. MyGo draws the UI with Metal on macOS, Direct3D 11 on Windows, and OpenGL on Linux; native file dialogs, menus, clipboard access, and tray integration use MyGo's platform APIs. Linux needs GTK 3 at runtime, and its tray integration additionally needs `libayatana-appindicator3`.

GUI logs use a stable writer so Settings can change the destination and level while the proxy is running. A configured file is appended with mode `0600`; missing parent directories are created with mode `0700`. An empty path uses stderr. Startup diagnostics retain only the latest 64 KiB, including when an individual write exceeds that limit, for the startup error message. Ongoing logging does not grow the diagnostic buffer or replay all logs at exit.

Compare separate processes with the same configuration, display scale, and window size when measuring memory. On Windows, Task Manager's Memory column and a process's private bytes measure different things; collect both and distinguish startup peaks from idle use. Go heap profiles do not include native graphics allocations, so also check process memory when comparing builds.

### GUI assets

`assets/ssh-keyselect-logo.png` is the source artwork. `just gen-platform-icons` creates the multi-size ICO, Linux desktop PNGs, and macOS ICNS under the ignored `assets/gui/generated/` directory. `just gen-windows-icons` also generates ignored Windows `.syso` resources under `assets/gui/generated/windows/`.

Source artwork is decoded lazily once. The application icon supplies the native default, while windows use a shared 32px image for sharp title-bar icons; the tray icon is generated at its requested size.

Each Windows executable combines its icon and version information in one resource object because the Go linker accepts only one resource section per executable. `assets/gui/windows-cli-resources.json` and `assets/gui/windows-gui-resources.json` define the shared `SSH KeySelect` file description and product name, the project copyright notice, and each executable's original filename. Windows version resources use `go-winres`, pinned in `scripts/generate-windows-icons.sh` and `scripts/generate-windows-resource.sh`. GoReleaser's per-target hooks use the latter script to override both executables' file and product versions with `.Version`, then remove the temporary resource after each build. Local builds use `0.0.0.0`. The GUI's resource description also supplies its `SSH KeySelect` name in Task Manager.

The main window title and tray tooltip include the displayed active Listen endpoint followed by ` - SSH KeySelect`. Both labels are refreshed when the Listen endpoint changes in Settings. The main window's `StateKey` lets MyGo restore its position and size between launches.

Because Go includes `.syso` files from a package directory, `just build`, `just snapshot`, `just release`, and release CI temporarily stage copies in the command directories and remove them after building.

The icon source currently has no recorded provenance or license metadata in this repository. Confirm that the project has redistribution rights and record its source/license, or replace it with a project-created asset, before publishing binaries.

## Development commands

```bash
just update
just lint
just test
just build
just snapshot
```

`just test` runs the non-GUI suite and the GUI-tagged tests. For focused work, use `go test ./path/to/package` and `go test -tags gui ./path/to/package`. Tests requiring OpenSSH binaries or platform transports are environment or OS specific.

Frontend connection-limit tests pass a cap of four connections to the shared serving implementation. This exercises admission, rejection, and slot reuse through real sockets without exhausting small file-descriptor budgets, while production `Serve` allows up to 128 connections.

Frontend timeout tests pass a 200 ms frame deadline to the shared serving and connection-handling implementation, so they exercise cancellation and socket cleanup without waiting for the production `Serve` deadline of 10 seconds. Each test supplies its own limits; no global limits are changed.

Signing approval tests pass a 200 ms upstream setup deadline and withhold approval for 300 ms. This checks that signing waits can exceed the setup deadline, for both bound and unbound sessions, without waiting for the production 10-second deadline. The tests use real transport connections and also verify cancellation when the client disconnects.

The GUI runtime integration check applies endpoint changes against real Unix sockets and verifies that the proxy moves to the replacement endpoint. Linux terminal tests use a real pseudoterminal to check context cancellation, the final newline after Escape, and input handoff to the next reader. Windows terminal tests cover both blocking inherited pipes and Go's overlapped pipes, checking that later input survives cancellation. The Unix foreign-directory permission test requires root and otherwise skips; it changes only a temporary fixture's owner.

Run `just deps-credits` after changing Go dependencies or the supported target operating systems. It installs a pinned `go-licenses` into a temporary directory, scans Linux, macOS, and Windows builds with the GUI tag, and regenerates `internal/credits/dependencies.txt` and `CREDITS`. The scanner is used because it can report GUI-tagged and platform-specific imports in each build. Check the output for newly introduced or changed licenses. The generated `CREDITS` includes third-party license texts for the modules used by shipped targets.

## Release packaging

`just clean` removes `dist/`, generated platform assets, and any temporary Windows resource copies in the command directories. `just build` creates local binaries and generated platform assets. `just snapshot` builds a local GoReleaser snapshot. `just release` runs GoReleaser without publishing.

Release archives contain both executables, `LICENSE`, and `CREDITS`. Linux packages install `LICENSE` and `CREDITS` under `/usr/share/doc/ssh-keyselect` and install the GUI desktop entry and PNG icons. Only macOS archives include `ssh-keyselect-gui.app` for Finder launches. The Darwin GUI post-build hook creates this bundle, and the archive file globs match both `.Os` and `.Arch` so it cannot be included in Linux or Windows archives. Windows release binaries embed their icon and use the GUI subsystem for the GUI executable.

RPM artifacts are signed by GitHub Actions using `RPM_SIGNING_KEY`; set `NFPM_PASSPHRASE` if the key requires a passphrase. The release flow is:

1. Run `git tag -s vX.Y.Z -m vX.Y.Z`.
2. Run `git push origin vX.Y.Z` and wait for the Release to be created.
3. Edit the created Release.
4. Press the `Generate release notes` button and edit the release notes.
5. Press the `Update release` button.

## Dependency license inventory

The application project uses Apache-2.0. Third-party modules include MIT, Apache-2.0, and BSD-3-Clause licensed packages; the compact About inventory and full notices are regenerated from the module graph for every shipped target. Keep their license notices in `CREDITS` when redistributing binaries.

The repository's top-level license does not establish rights to separately sourced media assets. Track provenance for icons, fonts, and other non-code resources independently.
