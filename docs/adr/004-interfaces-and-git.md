# ADR-004: Local library/CLI first, provider-independent Git later

> **Superseded by [ADR-013](013-database-storage.md)** (2026-10-24). Kept as the record of the file-first design.

Accepted. Demonstrate file interoperability through a typed Python API and CLI.
There is no HTTP endpoint until authentication, sessions, CSRF and preview
sanitization are designed and tested. This keeps Phase 1 reviewable.

Git is optional and downstream of files. Future commands use argument arrays,
explicit state checks and never implicit force/reset/clean/conflict resolution.
GitHub is not a content abstraction. Credentials must stay outside content and
Git history. This ADR is a boundary, not a claim that synchronization exists.
