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

Development began against Codex CLI `0.148.0-alpha.15`. The project does not use
that version's private OAuth endpoints. Login is delegated to the installed
official CLI, reducing the compatibility surface to its documented cached
authentication shape.

On an unknown or malformed schema, `codex-switch` stops before overwriting the
live file. Add a redacted fixture and a versioned adapter before broadening the
accepted shape.

## Upstream references

- OpenAI authentication documentation: https://developers.openai.com/codex/auth
- CC Switch managed Codex OAuth implementation:
  https://github.com/farion1231/cc-switch/tree/v3.20.0
