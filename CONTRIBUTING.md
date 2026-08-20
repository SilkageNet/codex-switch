# Contributing

1. Create a focused branch.
2. Never use real Codex credentials in fixtures or tests.
3. Run `pre-commit run --all-files`.
4. Run `go test -race ./...` and `go build ./...`.
5. Document user-visible behavior and compatibility changes.

Authentication format changes must fail closed, preserve unknown JSON fields,
and include redacted fixtures. New logging must be reviewed for credential
exposure.
