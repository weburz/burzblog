# ADR-002: Preserve source and use bounded round-trip YAML

Accepted. Retain the original frontmatter bytes when metadata is unchanged;
retain the body verbatim even when metadata changes. New documents use LF,
stable known-key ordering, sorted extra fields, and a final newline. Existing
documents keep line endings and their missing final newline. No-op saves return
identical bytes, including comments and quoting.

Use ruamel.yaml's round-trip mode, never an unsafe object constructor. Reject
aliases, anchors, explicit tags, directives, duplicate keys, deep nesting and
oversized documents. Unsupported documents fail visibly without being rewritten.
Metadata edits may normalize YAML whitespace but preserve unknown values and
comments where the library supports them. Markdown is not parsed or rendered.
A future visual editor must fall back to source mode for unsupported constructs.
