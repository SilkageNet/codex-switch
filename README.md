# codex-switch

`codex-switch` is a small, cross-platform account manager for local Codex clients.
It keeps one shared `CODEX_HOME` and switches only the active authentication
projection, so projects, sessions, plugins, skills, MCP configuration, and local
UI state stay in place.

> [!IMPORTANT]
> This is an independent compatibility tool, not an OpenAI product. The active
> credential is projected to the officially supported file store at
> `$CODEX_HOME/auth.json`. Treat that file like a password.

## Why

Codex supports cached ChatGPT login credentials, but exposes one active login at
a time. Logging out and completing the browser flow for every account switch is
slow. `codex-switch` stores multiple profiles in an encrypted local vault and
atomically projects the selected profile into the existing Codex home.

## Security model

- A random 256-bit vault key is stored in macOS Keychain, Windows Credential
  Manager, Linux Secret Service, or Windows DPAPI when running inside WSL2.
- Account bundles are encrypted at rest with XChaCha20-Poly1305.
- Only the active account is present in the Codex plaintext file store.
- Tokens are never printed by commands, JSON output, or diagnostics.
- Switches use a cross-process lock, compare-before-replace checks, and a
  recovery journal.
- The tool fails closed when the operating-system credential store is
  unavailable. It never falls back to a plaintext multi-account store.

The threat model trusts the current operating-system user. It reduces accidental
leaks, cross-user access, and unsafe writes; it cannot protect credentials from a
malicious process already running as the same user.

## Install from source

```bash
go install github.com/SilkageNet/codex-switch/cmd/codex-switch@latest
```

Release archives for macOS, Linux, and Windows are published on GitHub.

Adding accounts and querying live usage require an official Codex CLI that this
process can launch. On Windows, install the
[standalone Codex CLI](https://learn.chatgpt.com/docs/codex/cli); the executable
bundled inside the Codex desktop app's WindowsApps package cannot be launched by
external processes. Verify the installation with `codex --version` before using
those commands.

WSL2 is supported by the Linux archive. It uses the Windows user's DPAPI
protection through the built-in `powershell.exe`; a Linux desktop Secret Service
session is not required.

## Quick start

```bash
# Initialize the encrypted vault and enable Codex's file credential store.
codex-switch init --enable-file-store

# Preserve the login already active in Codex.
codex-switch account import-current personal

# Add another account through the official Codex login flow.
codex-switch account add work --device-auth

# Inspect and switch. Close Codex before switching, then restart it.
codex-switch account list
codex-switch account usage work
codex-switch use work
codex-switch current
```

Use `codex-switch doctor` before reporting a problem. Machine-readable output is
available on commands with `--json`.

## Usage without switching

`codex-switch` can inspect every saved account through the official Codex App
Server without making that account active:

```bash
# Query the active managed account now.
codex-switch account usage

# Query one saved account, or all accounts concurrently.
codex-switch account usage work
codex-switch account usage --all

# Refresh all rows in the compact account table.
codex-switch account list --refresh

# Work offline with the last successful snapshots.
codex-switch account list --cached
codex-switch account usage work --cached
```

Normal `account list` calls refresh only missing snapshots or snapshots older
than 60 seconds. Each query runs in an isolated temporary `CODEX_HOME`; it does
not switch `$CODEX_HOME/auth.json`, sessions, plugins, or UI state. The cache
contains usage numbers and public account metadata only, never tokens.

## Commands

```text
codex-switch init
codex-switch current
codex-switch status
codex-switch doctor
codex-switch use <alias>
codex-switch deactivate
codex-switch select

codex-switch account add <alias>
codex-switch account import-current <alias>
codex-switch account list [--refresh|--cached]
codex-switch account usage [alias] [--all] [--cached]
codex-switch account show <alias>
codex-switch account rename <old> <new>
codex-switch account reauth <alias>
codex-switch account remove <alias>

codex-switch vault export --output backup.cxs
codex-switch vault import --input backup.cxs
codex-switch vault rotate-key
codex-switch update [--check]
```

## What a switch changes

Normal account switches modify only:

- `$CODEX_HOME/auth.json`
- `codex-switch`'s own state and encrypted vault

An account-usage query may persist an officially refreshed credential generation
back to the encrypted vault. If the queried profile is active, the same
generation is safely reconciled into `$CODEX_HOME/auth.json`; no account selection
or Codex-owned state changes.

Initialization may make a one-time, backed-up change to
`$CODEX_HOME/config.toml` to set `cli_auth_credentials_store = "file"`.
`codex-switch` does not rewrite session history or Codex configuration during a
switch.

## Documentation

- [Architecture](docs/architecture.md)
- [Security](docs/security.md)
- [Compatibility](docs/compatibility.md)
- [Troubleshooting](docs/troubleshooting.md)

The design was informed by the MIT-licensed
[CC Switch](https://github.com/farion1231/cc-switch), especially its handling of
Codex CLI refresh-token rotation. This project is an independent, narrower
implementation and does not include CC Switch's provider routing or proxy stack.

## License

MIT
