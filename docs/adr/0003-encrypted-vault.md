# ADR 0003: Encrypt profiles with an OS-protected master key

Status: accepted

Complete token bundles are encrypted in one versioned local vault. A small
random master key is stored in the platform credential store. Storing every
bundle directly in Credential Manager was rejected because platform item-size
limits vary and JWTs can be large. Plaintext fallback was rejected.
