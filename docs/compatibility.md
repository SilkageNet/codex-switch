# Compatibility

## Supported authentication

The initial compatibility adapter accepts ChatGPT login documents with:

- `auth_mode = "chatgpt"`
- `tokens.account_id`
- `tokens.access_token`
- `tokens.refresh_token`
- optional `tokens.id_token`
- optional RFC3339 `last_refresh`

Unknown fields are retained because the complete raw document is encrypted and
projected. API-key profiles are intentionally outside the first release.

## Credential store

The active Codex credential store must be `file`. Run:

```bash
codex-switch init --enable-file-store
```

The command creates a timestamped backup before making a surgical top-level
`config.toml` edit. Normal account switches do not edit `config.toml`.

## WSL2

The Linux build detects WSL through the standard WSL environment and Microsoft
kernel markers. On WSL it prefers Windows PowerShell and current-user DPAPI over
Linux Secret Service:

- the generated vault key is encrypted for the current Windows user;
- only the DPAPI ciphertext is stored under
  `HKCU\Software\SilkageNet\codex-switch\secrets`;
- the key is sent to the static PowerShell bridge over standard input and is not
  placed in command-line arguments;
- no plaintext fallback file is created in the WSL filesystem.

Windows interoperability and the default `/mnt/c` mount must be enabled. Both
Windows PowerShell 5.1 (`powershell.exe`) and PowerShell 7 (`pwsh.exe`) are
recognized. If `secret-tool` is also available, it remains a compatibility
fallback so vaults created by earlier Linux builds can still be read and
rotated.

## Codex releases

Development began against Codex CLI `0.148.0-alpha.15`; isolated account-usage
queries were validated with `0.148.0-alpha.21`. The project does not use private
OAuth or usage endpoints. Login is delegated to the installed official CLI, and
usage is read through the documented stable Codex App Server protocol.

Usage querying initializes `codex app-server` and calls:

- `account/read`
- `account/rateLimits/read`
- `account/usage/read`

If one usage method is unavailable, the other is still cached and marked
partial. If both are unavailable, update the installed Codex client. These
methods require a ChatGPT/Codex service login; API-key-only and Amazon Bedrock
profiles are not supported by `codex-switch`.

On an unknown or malformed schema, `codex-switch` stops before overwriting the
live file. Add a redacted fixture and a versioned adapter before broadening the
accepted shape.

## Upstream references

- OpenAI authentication documentation: https://developers.openai.com/codex/auth
- OpenAI Codex App Server documentation:
  https://learn.chatgpt.com/docs/app-server
- CC Switch managed Codex OAuth implementation:
  https://github.com/farion1231/cc-switch/tree/v3.20.0
