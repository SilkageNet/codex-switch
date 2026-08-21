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
