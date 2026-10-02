# Frontmatter contract

UTF-8 Markdown may have an opening `---` line and closing `---` line. Files
without frontmatter are supported unchanged. Existing files need not contain
BurzBlog fields. When present:

| Field | Type / meaning |
| --- | --- |
| title | Nonempty string |
| slug | Lowercase ASCII words/digits separated by single hyphens |
| status | `draft` or `published`; metadata only, no deployment implied |
| date, updated | ISO date or timestamp; never automatically updated |
| authors, tags | Arrays of strings |
| description, image | Strings; image is inert metadata, not fetched |

New content requires title and slug and defaults to draft. Filename and slug
are independent: renaming does not silently modify metadata. Duplicate slugs
across files are not currently indexed or prohibited.

Unknown string-key fields with ordinary YAML scalars/lists/maps are preserved,
including dates. Duplicate keys, tags, aliases/anchors, directives, non-finite
numbers, sets, complex keys, more than 32 nesting levels or 10,000 parser events
are rejected. Frontmatter is limited to 64 KiB, whole documents to 2 MiB.
This is deliberately narrower than all of YAML. Rejection never rewrites a file.

An unchanged document returns exact original source. A body-only edit retains
the entire original frontmatter byte-for-byte. Metadata edits use a round-trip
parser; comments/order/quotes are retained where supported, but YAML indentation
and whitespace may change. Existing Markdown body, CRLF and missing final
newline are preserved. New documents use LF, stable field order and a final
newline. Markdown constructs, including raw HTML, are inert text in this phase.
