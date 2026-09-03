# Architecture

## Invariants

1. A user has one live `CODEX_HOME`.
2. Account switching projects only `auth.json`.
3. Codex-owned sessions, databases, configuration, plugins, skills, and UI state
   are never copied between profile directories.
4. The official `codex login` command owns the login protocol.
5. Unknown authentication schemas and ambiguous token generations fail closed.
6. The live `auth.json` identity is authoritative. Persisted active state is a
   recovery hint and must never override a different live account ID.

## Components

- `authschema` validates the supported ChatGPT login shape while retaining the
  complete raw JSON document, including unknown fields.
- `codexlogin` runs official login in a temporary `CODEX_HOME` configured for
  file storage, then imports the resulting document.
- `secretstore` protects a small random vault key with the operating-system
  credential store. WSL uses a Windows PowerShell bridge to protect the key with
  current-user DPAPI and store only ciphertext in HKCU.
- `vault` encrypts all saved account profiles with XChaCha20-Poly1305.
- `switcher` observes the live identity, classifies external-login and token
  drift, safely synchronizes known profiles, prepares a journal, performs
  compare-before-replace, and records the selected profile.
- `codexusage` runs the official Codex App Server in an isolated temporary
  `CODEX_HOME` and reads the stable account, rate-limit, and token-usage methods.
- `accountusage` queries up to four profiles concurrently, reconciles credential
  refresh generations, and coordinates with switching through the same lock.
- `usagecache` stores credential-free successful snapshots separately from the
  encrypted vault.
- `atomicfile` publishes complete files and refuses symlink destinations.
- `doctor` reports only redacted, non-secret local facts.

## Switch transaction

```text
acquire lock
  -> recover stale journal
  -> read and hash live auth
  -> identify the live account independently of recorded state
  -> reconcile live refresh generation into vault
  -> decrypt and validate target
  -> if target is already live, repair state without replacing auth.json
  -> otherwise verify Codex is stopped
  -> persist prepared journal
  -> compare live hash again
  -> atomically replace auth.json
  -> persist active state
  -> remove journal
```

The journal contains only profile IDs, hashes, and timestamps. If the process
stops after replacement but before state persistence, recovery compares the live
file with both hashes and completes the state transition.

## Live-state reconciliation

```text
read live auth and recorded state
  -> match account_id plus workspace_id against encrypted profiles
  -> prefer one exact credential-material match
  -> classify in-sync, external login, refresh, unmanaged, or ambiguous
  -> for sync: compare the live hash again
  -> adopt only a provably newer live generation
  -> persist the derived active pointer
```

Read-oriented commands use the observation immediately, so a stale recorded
profile never receives the active marker. Observation itself does not modify
state or credentials; the existing live usage-refresh path may still persist a
validated newer token generation. `sync` performs explicit reconciliation
writes under the shared lock. Unknown and multiple matches are never assigned
by email or alias, and conflicting token generations require `--prefer-live`.

## Isolated usage query

```text
acquire shared operation lock
  -> decrypt and validate selected profile(s)
  -> create one temporary CODEX_HOME per profile
  -> write only that profile plus file-store config
  -> initialize the official Codex App Server
  -> read account/rateLimits/read and account/usage/read
  -> retain quota reset times and any earned-reset count and expiration metadata
  -> stop the server and delete the temporary home
  -> reconcile any newer credential generation
  -> atomically save credential-free usage snapshots
```

The live account selection never changes. If Codex rotates a refresh token while
answering the query, identity and generation checks run before the new document
is saved. For an active profile, compare-before-replace protects the live
projection from a concurrent Codex write.

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

The usage cache is `usage-cache.v1.json` inside the resolved `codex-switch` data
directory. It contains no authentication documents.
