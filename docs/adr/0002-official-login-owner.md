# ADR 0002: Delegate login to the official Codex CLI

Status: accepted

`codex-switch` invokes `codex login` inside a temporary, file-backed
`CODEX_HOME`. It does not embed OAuth client identifiers or call private login
endpoints. The installed Codex version therefore owns browser/device login,
workspace policy, MFA, and protocol evolution.
