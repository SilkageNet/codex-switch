# Troubleshooting

Start with:

```bash
codex-switch doctor
```

The report contains no authentication material and is safe to inspect before
sharing. Review it manually because paths and account IDs may still be private.

## Credential store is auto or keyring

`codex-switch` needs the official file store for the active projection:

```bash
codex-switch init --enable-file-store
```

A timestamped `config.toml.codex-switch.bak.*` file is created first.

## WSL reports that secret-tool is unavailable

Update to a WSL-capable release and rerun initialization:

```bash
codex-switch update
codex-switch init --enable-file-store
```

WSL2 does not need `secret-tool`. `codex-switch` uses Windows DPAPI through the
Windows PowerShell executable and stores only encrypted bytes in the current
Windows user's registry.

If the updated command reports that PowerShell is unavailable, verify WSL
interoperability:

```bash
powershell.exe -NoLogo -NoProfile -Command '$PSVersionTable.PSVersion'
```

If that executable cannot run, enable Windows interoperability and the `/mnt/c`
mount in WSL, restart the distribution with `wsl.exe --shutdown` from Windows,
and retry. Installing `libsecret-tools` alone is not sufficient unless the WSL
distribution also runs a working Secret Service and DBus session.

## Codex is still running

Quit the desktop app and stop active `codex` CLI processes. The tool refuses the
switch because a running Codex process could refresh and rewrite the outgoing
credential after replacement.

`--allow-running` exists for recovery and expert use. It is not recommended for
ordinary switching.

## Active account is unmanaged

Preserve it before switching:

```bash
codex-switch account import-current current
```

## Token generations are ambiguous

Codex changed a refresh token but the saved and live timestamps cannot prove
which is newer. Reauthenticate the affected profile:

```bash
codex-switch account reauth <alias>
```

The ambiguity is intentionally not resolved by guessing.

## Account usage is unavailable

Confirm the official Codex executable is installed and current:

```bash
codex --version
codex-switch account usage <alias>
```

Usage queries require network access and a saved ChatGPT login. They do not work
for API-key-only or Amazon Bedrock authentication. A missing method on an older
Codex build is reported as partial when the other method still works; update
Codex if both usage methods are unavailable.

For an offline or temporarily failing service, inspect the last successful
snapshot without making a request:

```bash
codex-switch account list --cached
codex-switch account usage <alias> --cached
```

A stale cached snapshot is labeled `stale`; a failed refresh keeps that snapshot
and displays a warning rather than discarding useful data.

## Interrupted switch journal

Run `codex-switch status`. Recovery compares the current `auth.json` hash with
both sides of the journal and either rolls state forward or discards an
uncommitted journal. If neither hash matches, preserve the files and open a
private support report.
