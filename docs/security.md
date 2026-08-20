# Security model

## Protected assets

- ChatGPT access tokens
- ChatGPT refresh tokens
- ID tokens and their account metadata
- portable vault backups

Saved profiles are encrypted with XChaCha20-Poly1305. A separate random key is
stored in macOS Keychain, Windows Credential Manager, or Linux Secret Service.
The key is never stored next to the ciphertext.

The active profile must be readable by Codex and is therefore projected into
the officially supported plaintext file store. That file is created with mode
`0600` on Unix. On Windows it lives in the current user's profile and is
published with a replace-existing operation.

## Threat boundaries

The current operating-system user is trusted. A malicious process running as
that user can observe or use the same credentials Codex can use. The project
focuses on:

- preventing multi-account secrets from entering ordinary JSON/config files;
- avoiding accidental logs, shell history, issue attachments, and repository
  commits;
- preventing other local users from reading stored files;
- preventing partial, stale, concurrent, and path-traversing writes.

## Operational rules

- Commands never expose a token retrieval operation.
- JSON output is intentionally based on dedicated public view types.
- Real credentials are forbidden in tests and fixtures.
- The Linux desktop implementation fails closed when Secret Service is absent.
- Portable backups require a passphrase of at least 12 characters and use
  Argon2id before XChaCha20-Poly1305 encryption.
- Authentication documents larger than the configured limit are rejected.
- A symlink at a managed destination is rejected instead of followed.

## Platform credential-store adapters

The macOS implementation invokes the system `security` utility without a shell.
Because that utility treats `-w` without a value as an interactive prompt rather
than reading standard input, the adapter supplies the vault key directly to
`-w`. The value can therefore be visible briefly to processes running as the
same operating-system user, which is inside this project's trust boundary.
Linux sends values to `secret-tool` over standard input. Windows calls the
native Credential Manager API directly.
