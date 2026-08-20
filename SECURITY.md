# Security policy

Please do not open public issues containing `auth.json`, access tokens, refresh
tokens, ID tokens, exported vaults, screenshots of credentials, or diagnostic
archives that have not been reviewed.

Report suspected vulnerabilities privately through GitHub Security Advisories.
Include the affected version, platform, and a minimal reproduction that contains
no real credentials.

Supported versions are the latest stable release and the current development
branch. Security fixes may intentionally reject older vault or authentication
formats rather than attempting a lossy migration.

## Upstream Go advisories

CI temporarily recognizes `GO-2026-5026`, `GO-2026-5972`, `GO-2026-6090`, and
`GO-2026-6218` while the Go vulnerability database lists their fix as Go
1.26.6 but the latest published stable toolchain is Go 1.26.5. Any additional
finding still fails CI. The affected network surface is restricted to fixed
HTTPS GitHub release endpoints and bounded responses. This exception must be
removed as soon as Go 1.26.6 is available.
