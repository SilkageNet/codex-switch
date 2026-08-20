# ADR 0001: Keep one Codex home

Status: accepted

Account switching changes only the active authentication projection. Separate
`CODEX_HOME` directories were rejected because they fragment sessions, plugins,
skills, MCP configuration, and UI state—the exact data this project is intended
to share.
