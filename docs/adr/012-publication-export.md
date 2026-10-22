# ADR-012: Versioned public projection for reader sites

Status: accepted for launch preparation.

## Context

An editor status change and Git transport do not establish what is safe to serve
publicly. Consumers otherwise have to rediscover draft filtering, metadata
privacy, duplicate URLs, and failed-release behavior independently. Application
code and editorial content must remain separate installation concerns.

## Decision

Add a local, read-only `burzblog export` command that validates a stable content
copy and emits a bounded schema-v1 JSON projection. Explicit published status,
required title/slug, unique published slugs, and a public metadata allowlist are
mandatory. Malformed inventory and unintended empty output fail without JSON.
Consumers reject unsupported schemas, sanitize rendered bodies, and promote
complete successful builds only. The static reader example demonstrates this
boundary using existing dependencies.

Publish in the authenticated editor remains a metadata operation. No automatic
Git push, deployment hook, public API, or media upload is introduced. Git remains
an optional transport to a separate private content repository. Deployments can
also supply a stable filesystem copy without Git. Authentication SQLite remains
outside content and contains no canonical articles.

## Consequences

Unknown metadata is preserved in editorial files but excluded from public output.
Adding a public field is a reviewed contract decision. Published body text and
allowed fields are public by intent; filtering cannot detect private prose.
Image/video URLs remain text references. Reader rendering may load images, while
the private preview continues to suppress them.

Export is bounded and reproducible, not a multi-file filesystem transaction.
Use a quiescent copy; an inventory recheck catches ordinary changes but is not
a substitute for this precondition. A reader deployment is responsible for
atomic promotion, rollback, and cache invalidation on withdrawal. Tests verify
the local boundary; production hosting still needs separate acceptance evidence.

See [publishing.md](../publishing.md) for the consumer contract and acceptance
checks, and [launch.md](../launch.md) for release gates.
