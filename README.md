<div align="center">
  <img src="assets/ssh-keyselect-logo.png" alt="SSH KeySelect" width="128" height="128">
</div>

# SSH KeySelect

Selective SSH Agent Proxy

[![Tag](https://img.shields.io/github/tag/jfut/ssh-keyselect.svg)](https://github.com/jfut/ssh-keyselect/releases)
[![License](https://img.shields.io/badge/license-Apache%202-blue)](https://github.com/jfut/ssh-keyselect/blob/main/LICENSE)

`SSH KeySelect` is an interactive SSH agent proxy that lets you choose which keys an SSH client can offer from your existing agent. By rejecting signing requests for unselected keys, it helps mitigate **SSH Agent Hijacking**.

Private keys stay in your existing agent. Offering fewer keys can reduce the risk of reaching an SSH server's `MaxAuthTries` limit.

## Overview

### SSH connections from your machine

SSH clients that use the `SSH_AUTH_SOCK` environment variable can use KeySelect when connecting to an SSH server. That includes direct use of `ssh` and OpenSSH launched by `git`, `rsync`, or the VS Code `Remote - SSH` extension.

The client requests available identities, and the picker controls which ones it receives. When the client needs a signature, KeySelect forwards the request to the upstream agent. Private key material stays there.

### Through agent forwarding

With agent forwarding and `Auto Select Off`, a remote process such as Git or a nested SSH client uses its forwarded `SSH_AUTH_SOCK`. Its requests travel through the SSH connection to your local SSH client and KeySelect. The picker runs locally, and permitted signing requests go to your local upstream agent.

> [!IMPORTANT]
> Forwarding your agent creates an **SSH Agent Hijacking** risk: even though private keys stay on your machine, a remote host can request signatures from your agent. A malicious or compromised host can use those signatures to access SSH services and Git hosts reachable with your credentials, including reading or pushing to repositories. Forward your agent only to hosts you trust. See the [OpenSSH manual](https://man.openbsd.org/ssh) for its warning about agent forwarding.

## Installation

SSH KeySelect does not install a background service or enable automatic startup. It creates its Listen endpoint while running.

### Windows

1. Download a Windows archive from [Releases](https://github.com/jfut/ssh-keyselect/releases). It includes `ssh-keyselect.exe` for terminal use and `ssh-keyselect-gui.exe`.
2. Extract it into a directory you control.
3. Add the executable directory to `PATH` to run the CLI.

Windows release executables are currently unsigned. See the [Code signing policy](#code-signing-policy) for the current status.

### macOS

1. Download a macOS archive from [Releases](https://github.com/jfut/ssh-keyselect/releases). It includes `ssh-keyselect` for terminal use and `SSH KeySelect.app` for Finder.
2. Extract it into a directory you control.
3. Verify the official archive against `checksums.txt`.
4. Add the executable directory to `PATH` to run the CLI.
5. After following the note below, launch `SSH KeySelect.app` from Finder or move it to your Applications folder.

> [!CAUTION]
> The macOS app and CLI are not signed with a Developer ID or notarized, so Gatekeeper may block them after download. After extracting the archive and verifying its checksum, run these commands from the extracted directory before opening the app or CLI:
>
> ```sh
> xattr -dr com.apple.quarantine "SSH KeySelect.app"
> xattr -dr com.apple.quarantine ssh-keyselect
> ```
>
> These commands remove the download quarantine attribute; they do not add a trusted code signature. Once Developer ID signing and notarization are enabled, you can skip them but should continue verifying the archive checksum. See the [Code signing policy](#code-signing-policy) for the current status.

### Linux

1. Download a Linux archive from [Releases](https://github.com/jfut/ssh-keyselect/releases). It includes `ssh-keyselect` for terminal use and `ssh-keyselect-gui`.
2. Extract it into a directory you control.
3. Add the executable directory to `PATH` to run the CLI.

#### RHEL-compatible distributions

Download an RPM from [Releases](https://github.com/jfut/ssh-keyselect/releases) and install it directly, or configure the repository for DNF-managed installation.

RPM installation places the executables in `/usr/bin` and installs a desktop launcher, icons, and license notices in system directories. Run RPM and repository setup commands with administrative privileges.

To install a downloaded RPM:

```bash
# x86_64
dnf install ./ssh-keyselect-x.y.z-n.x86_64.rpm

# aarch64
dnf install ./ssh-keyselect-x.y.z-n.aarch64.rpm
```

To configure the repository, first install [dnf-plugin-anyrepo](https://github.com/jfut/dnf-plugin-anyrepo), then run the commands below. They add jfut's public signing key to the RPM keyring, register an AnyRepo repository, and install the package.

```bash
rpm --import https://raw.githubusercontent.com/jfut/ssh-keyselect/refs/heads/main/RPM-GPG-KEY-jfut-github
dnf-anyrepo add https://github.com/jfut/ssh-keyselect
dnf install ssh-keyselect
```

See [Uninstallation](#uninstallation) for removal instructions.

#### Linux GUI runtime requirements

The Linux GUI requires GTK 3. A system tray icon also requires `libayatana-appindicator3` and a desktop environment that displays AppIndicator icons. GNOME Shell does not show these icons by default.

On AlmaLinux 9 with GNOME, enable EPEL and install the AppIndicator library and GNOME Shell extension:

```bash
dnf config-manager --set-enabled crb
dnf install epel-release
dnf install libayatana-appindicator-gtk3 gnome-shell-extension-appindicator

# Enable the extension for each user
gnome-extensions enable appindicatorsupport@rgcjonas.gmail.com
```

Sign out and back in if the icon does not appear after enabling the extension. The main window works without the AppIndicator library or extension.

#### Other Linux package formats

Releases also include native packages for Debian and Ubuntu (`.deb`), Alpine Linux (`.apk`), and Arch Linux (`.pkg.tar.zst`). Termux packages are provided as `.deb` files. Download the package for your architecture from [Releases](https://github.com/jfut/ssh-keyselect/releases) and install it with `apt`, `apk`, or `pacman`, as appropriate.

The Alpine `.apk` is unsigned. Download `checksums.txt` with the package and verify the package checksum before installing it. Replace the example filename below with the exact `.apk` asset you downloaded:

```sh
apk_file=ssh-keyselect_VERSION_ARCH.apk
grep -F "  $apk_file" checksums.txt | sha256sum -c
apk add --allow-untrusted "./$apk_file"
```

## Quick start

### GUI: ssh-keyselect-gui

Start the GUI first, then set `SSH_AUTH_SOCK` to the Listen endpoint shown in its main window. On macOS and Linux, the default endpoint is `$HOME/.ssh/ssh-keyselect-agent.sock`.

`Upstream` is the endpoint for your existing SSH agent; `Listen` is where SSH clients connect to KeySelect. The cards show these paths, and their `Copy export command` buttons copy POSIX `export` assignments with shell-quoted paths.

The GUI can start without an upstream agent. Configure its endpoints in Settings.

#### Shell

For Git Bash ([Git for Windows](https://gitforwindows.org/)) and Linux, start the GUI and set `SSH_AUTH_SOCK` to its default Listen endpoint:

```bash
ssh-keyselect-gui &
export SSH_AUTH_SOCK="$HOME/.ssh/ssh-keyselect-agent.sock"
ssh user@example.org
```

#### VS Code on Windows

Configure VS Code's [terminal profile](https://code.visualstudio.com/docs/terminal/profiles) to use Git Bash. Start the GUI with Listen mode set to `cygwin` and a Listen endpoint that matches `SSH_AUTH_SOCK` below. See the [Git Bash configuration example](#configuration) for setting the endpoint. Open User Settings (JSON) and add the following properties inside the existing top-level object. Keep your other settings. If any of these keys already exist, update them instead of adding duplicates; merge the `Git Bash` profile and `SSH_AUTH_SOCK` into the existing profile and environment objects. Replace `USERNAME` with your Windows account name:

```json
"terminal.integrated.profiles.windows": {
  "Git Bash": {
    "path": "C:/Users/USERNAME/scoop/apps/git-with-openssh/current/bin/bash.exe"
  }
},
"terminal.integrated.defaultProfile.windows": "Git Bash",
"terminal.integrated.env.windows": {
  "SSH_AUTH_SOCK": "C:/Users/USERNAME/.ssh/ssh-keyselect-agent.sock"
}
```

#### Windows OpenSSH

For Windows OpenSSH, set the GUI's Listen mode to Named Pipe. The default endpoint is `\\.\pipe\ssh-keyselect-agent.socket`.

If you changed the Windows Listen endpoint in Settings, use the endpoint shown in the GUI instead of the default named pipe above.

For Windows PowerShell:

```powershell
Start-Process ssh-keyselect-gui.exe
$env:SSH_AUTH_SOCK = "\\.\pipe\ssh-keyselect-agent.socket"
ssh user@example.org
```

For Windows Command Prompt:

```bat
start "" ssh-keyselect-gui.exe
set SSH_AUTH_SOCK=\\.\pipe\ssh-keyselect-agent.socket
ssh user@example.org
```

### CLI: ssh-keyselect

Run OpenSSH through a temporary agent proxy. The picker appears in the current terminal, and the temporary endpoint is removed when SSH exits.
An upstream agent must be available through `UPSTREAM_SSH_AUTH_SOCK` or `SSH_AUTH_SOCK`, or set explicitly with `--upstream`.

```bash
ssh-keyselect ssh user@example.org
ssh-keyselect ssh -- -p 2222 user@example.org
```

To use the picker for terminal commands named `ssh`, add an alias:

```bash
alias ssh='ssh-keyselect ssh'
```

Git can use the same command:

```bash
GIT_SSH_COMMAND='ssh-keyselect ssh' git clone git@example.org:owner/repo.git
git config core.sshCommand 'ssh-keyselect ssh'
```

For `scp` and `sftp`, use an executable wrapper that runs `ssh-keyselect ssh`, then pass its path with `-S`.

> [!NOTE]
> The quick-start examples can change `SSH_AUTH_SOCK`, add an `ssh` alias, or set `GIT_SSH_COMMAND` or `core.sshCommand`. See [Uninstallation](#uninstallation) to restore or remove those changes.

## CLI

### Commands

|                          Command                           |                        Purpose                         |
| ---------------------------------------------------------- | ------------------------------------------------------ |
| `ssh-keyselect ssh [options] [--] [SSH arguments...]`      | Run OpenSSH through the terminal picker.               |
| `ssh-keyselect list [options]`                             | Print identities from the upstream agent.              |
| `ssh-keyselect test [options]`                             | Check the upstream agent and summarize its identities. |
| `ssh-keyselect version`                                    | Print the version and commit.                          |
| `ssh-keyselect --help` or `ssh-keyselect <command> --help` | Show commands and options.                             |

Long comments in CLI tables are shortened with an ellipsis for display. The original comments remain available to SSH clients.

### Options and environment

The wrapper options go before `--`; arguments after it are passed to OpenSSH.

- `ssh-keyselect ssh`: `--upstream`, `--upstream-mode`, `--listen`, `--listen-mode`, `--selection-timeout`, `--log-level`, `--log-file`
- `ssh-keyselect list` and `ssh-keyselect test`: `--upstream`, `--upstream-mode`

The terminal commands `ssh`, `list`, and `test` do not read TOML configuration files.

Unless `--upstream` is set, they use the first non-empty value from `UPSTREAM_SSH_AUTH_SOCK`, then `SSH_AUTH_SOCK`. Paths support environment-variable expansion and a leading `~`.

### Logging

Logging is off by default. Pass `--log-level` to use `debug`, `info`, `warn`, or `error`.

For an SSH session, pass `--log-file FILE` to write KeySelect diagnostic logs to a file instead of the terminal:

```bash
ssh-keyselect ssh --log-level debug --log-file ~/ssh-keyselect.log -- user@example.org
```

The log file is replaced on each run and uses mode `0600` on Unix-like systems. OpenSSH output remains in the terminal. Review logs before sharing them; they can include SSH host key fingerprints and request metadata.

### Terminal picker

The terminal picker displays, in order:

- A blank line and `[ssh-keyselect - YYYY-MM-DD HH:MM:SS TZ]`.
- The request context and a cyan `Select an SSH key to allow:` prompt above the key table.
- A gray `Filter (If you don't recognize this request, press Esc to cancel.)` hint on the empty filter line. The cursor starts at the beginning, and the hint disappears when you type.
- Host details indented beneath their tree labels.

|         Key         |              Action              |
| ------------------- | -------------------------------- |
| Type                | Filter the list.                 |
| Enter               | Select the highlighted identity. |
| Esc or Ctrl+C       | Cancel.                          |
| Backspace or Delete | Edit the filter.                 |
| Ctrl+U              | Clear the filter.                |

## GUI

### Command options

On Windows and Linux, run `ssh-keyselect-gui` to start the GUI and pass command options. macOS users launch `SSH KeySelect.app` from Finder and configure it through the GUI menus and Settings.

|       Command       |                                         Options                                         |
| ------------------- | --------------------------------------------------------------------------------------- |
| `ssh-keyselect-gui` | `--config`, `--listen`, `--listen-mode`, `--upstream`, `--upstream-mode`, `--log-level` |

### Configuration

The GUI reads TOML from `$XDG_CONFIG_HOME/ssh-keyselect/config.toml` when `XDG_CONFIG_HOME` is set. Otherwise, it uses the platform's user configuration directory:

| Platform |                                                Default path                                                 |
| -------- | ----------------------------------------------------------------------------------------------------------- |
| Windows  | `%APPDATA%\ssh-keyselect\config.toml` (usually `C:\Users\<user>\AppData\Roaming\ssh-keyselect\config.toml`) |
| Linux    | `~/.config/ssh-keyselect/config.toml`                                                                       |
| macOS    | `~/Library/Application Support/ssh-keyselect/config.toml`                                                   |

On Windows and Linux, use `--config FILE` to select another file; command-line values override file values. On macOS, open or save configuration files through the GUI's File menu. The following environment variables also affect endpoint paths:

- `SSH_KEYSELECT_LISTEN` overrides `agent.listen`.
- When `agent.upstream` is empty, `UPSTREAM_SSH_AUTH_SOCK` takes precedence over `SSH_AUTH_SOCK`.
- Environment variables and a leading `~` are expanded in configured paths using the GUI process environment.
- If a filesystem entry already exists at the configured Listen path when the GUI starts, the proxy starts as `Not configured` and leaves that entry untouched.

The configuration loader rejects unknown keys. Logging is off by default. Set `log.level` to enable it and optionally set `log.file` to append logs to a file. When `log.file` is empty, logs go to standard error.

The key selection timeout defaults to 120 seconds. In GUI Settings, set `agent.selection_timeout`; for `ssh-keyselect ssh`, pass `--selection-timeout SECONDS`. The timeout starts after the available identities are retrieved and before the selector queue is entered. Waiting behind another open picker counts toward the timeout, so the GUI countdown can start below the configured value or a request can expire before its picker appears.

If, after a timeout, the SSH client prints the errors below and the server's `/var/log/secure` contains `penalty: exceeded LoginGraceTime`, set the Key selection Timeout to match that server's `LoginGraceTime` value in seconds.

```text
kex_exchange_identification: read: Software caused connection abort
banner exchange: Connection to <IP> port <Port>: Software caused connection abort
```

Example configuration for `Git Bash` on Windows, using a Cygwin-compatible listener:

```toml
[agent]
# Replace alice with your Windows user name:
upstream = 'C:\Users\alice\.ssh\ssh-upstream-agent.sock'
listen = 'C:\Users\alice\.ssh\ssh-keyselect-agent.sock'

# macOS and Linux:
# upstream = "$SSH_AUTH_SOCK"
# listen = "$HOME/.ssh/ssh-keyselect-agent.sock"

upstream_mode = "auto"
listen_mode = "cygwin"
selection_timeout = 120 # seconds (1 to 86399)

[log]
file = ""
level = "off"
```

### Cross-shell setup

When using Git Bash with endpoints from different environments, set the modes explicitly:

```bash
ssh-keyselect-gui --upstream-mode cygwin --listen-mode cygwin --upstream ~/.ssh/ssh-upstream-agent.sock &
export SSH_AUTH_SOCK="$HOME/.ssh/ssh-keyselect-agent.sock"
ssh user@example.org
```

### GUI controls

- Open Settings from the Settings button or File > Settings. In Key selection, set how many seconds a key request can wait for a choice (default 120); the picker shows a live countdown. If it reaches zero, the expired dialog stays open until dismissed, and its key choice is no longer available. In Logging, set the log level and optional log file. Leaving the file empty writes logs to standard error.
- Apply activates endpoint and logging changes immediately. Use File > Save to store them.
- The File menu opens, saves, and saves as TOML configuration files. The GUI prompts before closing with unsaved changes.
- On macOS, minimizing the main window hides it from the Dock while keeping the menu bar icon available. Click the icon to show the window again.
- About shows the application version and third-party library license inventory. Full license notices are included in `CREDITS`.

### Auto Select

Auto Select temporarily bypasses the per-connection picker. When On, the proxy returns every upstream identity and forwards signing requests for any of them.

> [!CAUTION]
> When Auto Select is On, KeySelect no longer mitigates **SSH Agent Hijacking**: any client that can access the proxy can request signatures from every key in the upstream agent.

The GUI has an Auto Select switch beside Agent Proxy. Enabling it requires confirmation. Use it only for trusted work, then turn it off. The setting is temporary, is not saved in TOML, and applies to new identity requests. A client that already received all identities while Auto Select was on keeps that selection until its connection closes.

### GUI picker

The window title combines the action, application name, and local display time. The request-context area begins with the same timestamp followed by host details. It lets you scroll to earlier hops. Use the arrow keys to move through matches, Enter to select, and Esc to cancel.

Right-click a key row and choose `Copy`, or press Ctrl+C, to copy its Comment, Type, Size, and Fingerprint as tab-separated text.

## Shared behavior

### Endpoint modes

`--upstream-mode` controls how KeySelect connects to the existing agent. `--listen-mode` controls how the SSH client connects to the proxy.

|     Mode     |                              Endpoint                               |
| ------------ | ------------------------------------------------------------------- |
| `auto`       | Detect a compatible endpoint for the current shell and path.        |
| `unix`       | Unix-domain socket or Windows AF_UNIX socket.                       |
| `cygwin`     | Cygwin and Git for Windows socket files.                            |
| `wsl1`       | WSL1-compatible socket paths on Windows.                            |
| `named-pipe` | Named Pipe (Windows OpenSSH), such as `\\.\pipe\openssh-ssh-agent`. |

Both modes default to `auto` in the CLI and configuration. GUI Settings resolves an automatic listener mode to a concrete mode for display. Set a mode explicitly when connecting across shells or when automatic detection does not match the endpoint.

### Private key selection

The picker lets you choose a key available through the agent. The SSH agent protocol calls this entry an `identity`: a public key and comment that identify a key the agent can use to sign.

KeySelect forwards signing requests for selected keys to the upstream agent and never receives private key material.

The picker shows each key's comment, key type, size in bits, and SHA256 fingerprint. Selection is scoped to an SSH session-binding chain:

- With Auto Select Off, a new connection starts a fresh selection. A forwarding hop that follows an authentication binding starts a new selection.
- The selected keys are cached for that chain. Signing requests for unselected keys are rejected.
- Canceling returns no keys to the SSH client.

See [the developer notes](docs/dev.md#agent-proxy-request-flow) for the request and connection flow.

### Request context

Both pickers show request context as selectable, copyable text. For accepted `session-bind@openssh.com` messages, it includes verified host key types and fingerprints, and whether each connection is marked as forwarded. Forwarding hops and the current target appear in a tree to show connection boundaries.

The agent protocol does not provide a hostname. Names matched from the default local `known_hosts` files are hints, not verified hostnames. If no session binding is available or accepted, the picker says no verified host key was provided. Press Esc to cancel an unrecognized request.

### Security and limitations

- The proxy supports listing identities, signing with selected identities, and verified OpenSSH session bindings. Key-management and unknown agent requests are always rejected.
- The proxy never stores private keys or signs data. It forwards signing requests for permitted identities to the upstream agent.
- KeySelect can only filter identities provided by the upstream agent. Identity-listing and signing requests omit the destination host, user, and port. A session binding can provide a signed host key, but not a hostname, user, or port, so KeySelect cannot select keys by destination.
- On macOS and Linux, Unix socket files use mode `0600`, and a created parent directory uses mode `0700`.
- Keep Cygwin-compatible socket files in a directory accessible only to your account.

## Uninstallation

1. Quit the GUI and finish SSH sessions started through the CLI wrapper.
2. Restore shell and Git changes:
   - Restore the previous `SSH_AUTH_SOCK` value or remove the setting from shell startup files and VS Code User Settings (JSON). If you changed VS Code's default profile for this setup, restore its previous value; remove the Git Bash profile if you added it only for KeySelect and no longer need it.
   - Restore earlier `ssh`, `GIT_SSH_COMMAND`, and `core.sshCommand` values. If you added only this README's examples, run `unalias ssh` and, in each affected repository, `git config --local --unset core.sshCommand`.
3. Remove KeySelect wrappers, `PATH` entries, shortcuts, and program files. For archive installs, delete the distribution directory with its `LICENSE` and `CREDITS` files, plus any copied `SSH KeySelect.app`. For package installs, remove the package with the package manager you used to install it.
4. Optionally delete the GUI configuration files (see [Configuration](#configuration)), including files saved with Save As or `--config`, and diagnostic logs created with `--log-file`. Keep your upstream agent and SSH keys; they belong to your SSH setup.

For RHEL-compatible Linux distributions:

```bash
dnf remove ssh-keyselect
```

If you added the DNF repository during installation, remove it with:

```bash
dnf-anyrepo remove ssh-keyselect
```

The imported jfut RPM signing key and `dnf-plugin-anyrepo` can be shared by other packages and repositories. Keep them if you still use them.

## Code signing policy

See [CODE_SIGNING_POLICY.md](CODE_SIGNING_POLICY.md) for the current status, planned scope, and team responsibilities.

## Privacy policy

SSH KeySelect has no telemetry, analytics, automatic update checks, or diagnostic uploads. It communicates with other systems only for agent and SSH operations requested or configured by the user or operator.

The proxy communicates with SSH clients and the upstream agent configured through its endpoints. SSH clients receive public key identities and permitted authentication signatures. With SSH agent forwarding, remote processes can receive these responses through your SSH connection. Private key material stays in the upstream agent and is never received by KeySelect. See [Security and limitations](#security-and-limitations) and the [agent forwarding guidance](#through-agent-forwarding).

The GUI stores configuration locally when you save it. The pickers can read local `known_hosts` files to display host-name hints.

Diagnostic logging is off by default. When enabled, logs go to local output or the CLI log file you specify and can contain endpoint paths, host key fingerprints, and request metadata. Review logs before sharing them. Bundled libraries do not add third-party online services to the application. Your upstream agent and SSH client have their own settings and privacy policies.

## Code of Conduct

Contributors are expected to follow the project's [Code of Conduct](CODE_OF_CONDUCT.md).

## License

Apache-2.0

Copyright contributors to the ssh-keyselect project.

## Author

Jun Futagawa (jfut)
