# ADR-001: Markdown is canonical; operational state is secondary

Accepted. UTF-8 `.md` files contain body and portable frontmatter. No database
is required to recover, edit or publish content. Future indexes are disposable;
future SQLite operational state must remain separate. This preserves ordinary
Git and static-site-generator workflows at the cost of filesystem concurrency
constraints. There is no export step and no proprietary document format.
