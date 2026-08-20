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

## Interrupted switch journal

Run `codex-switch status`. Recovery compares the current `auth.json` hash with
both sides of the journal and either rolls state forward or discards an
uncommitted journal. If neither hash matches, preserve the files and open a
private support report.
