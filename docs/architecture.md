# Architecture

## Invariants

1. A user has one live `CODEX_HOME`.
2. Account switching projects only `auth.json`.
3. Codex-owned sessions, databases, configuration, plugins, skills, and UI state
   are never copied between profile directories.
4. The official `codex login` command owns the login protocol.
5. Unknown authentication schemas and ambiguous token generations fail closed.

## Components

- `authschema` validates the supported ChatGPT login shape while retaining the
  complete raw JSON document, including unknown fields.
- `codexlogin` runs official login in a temporary `CODEX_HOME` configured for
  file storage, then imports the resulting document.
- `secretstore` protects a small random vault key with the operating-system
  credential store.
- `vault` encrypts all saved account profiles with XChaCha20-Poly1305.
- `switcher` reconciles a live Codex refresh generation, prepares a journal,
  performs compare-before-replace, and records the selected profile.
- `atomicfile` publishes complete files and refuses symlink destinations.
- `doctor` reports only redacted, non-secret local facts.

## Switch transaction

```text
acquire lock
  -> recover stale journal
  -> verify Codex is stopped
  -> read and hash live auth
  -> reconcile live refresh generation into vault
  -> decrypt and validate target
  -> persist prepared journal
  -> compare live hash again
  -> atomically replace auth.json
  -> persist active state
  -> remove journal
```

The journal contains only profile IDs, hashes, and timestamps. If the process
stops after replacement but before state persistence, recovery compares the live
file with both hashes and completes the state transition.

## Data locations

`CODEX_HOME` resolution:

1. `--codex-home`
2. `CODEX_HOME`
3. `~/.codex`

`codex-switch` data resolution:

1. `--home`
2. `CODEX_SWITCH_HOME`
3. the operating system's user configuration directory

Only tests and advanced portable installations should normally override these
paths.
