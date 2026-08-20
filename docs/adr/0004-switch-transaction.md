# ADR 0004: Compare, replace, and recover

Status: accepted

Codex does not participate in the tool's lock and can rotate credentials while
running. Switches therefore require Codex to be stopped by default, reconcile
the outgoing refresh generation, compare the live file immediately before
replacement, publish atomically, and retain a non-secret recovery journal until
active state is committed.
