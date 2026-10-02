# BurzBlog

A lightweight, file-first Markdown CMS in development. Your files remain the
source of truth. **This repository currently implements the Phase 1 content
library and local CLI, not a deployable browser CMS.**

```text
Today:  Local CLI / Python API → ordinary Markdown files ←→ external editor / Git
Later:  Browser → authenticated application → same files ←→ optional Git remote
```

If BurzBlog disappears, nothing needs exporting: keep using your Markdown files
and ordinary Git repository.

## Development

Prerequisites: Python 3.12+, [uv](https://docs.astral.sh/uv/),
[Task](https://taskfile.dev/), Node 24 LTS for Pyright, and a local POSIX
filesystem. Docker is optional; the Python runtime itself does not need Node,
a database, credentials or external services.

```sh
task setup
task dev
task check
```

`uv.lock` locks dependencies. `task check` runs formatting, lint, strict typing,
unit/integration/CLI tests, and package build. Browser E2E tests are deferred
because there is no browser interface. GitHub Actions is intentionally disabled;
the dormant workflow targets Python 3.12 and 3.14. Local checks gate merges.
The build backend is pinned in `pyproject.toml`. See [tooling](docs/tooling.md)
for UniBurz-aligned Ruff, mypy, Pyright, pre-commit and Crisp commands.

## Try the file contract

```sh
mkdir -p demo-content/posts demo-content/.archive
printf '# Hello\n\nOrdinary Markdown.\n' | uv run burzblog --root demo-content create posts/hello.md --title Hello --slug hello
cat demo-content/posts/hello.md
printf '\nEdited outside BurzBlog.\n' >> demo-content/posts/hello.md
uv run burzblog --root demo-content read posts/hello.md
uv run burzblog --root demo-content list
```

`read` returns JSON containing complete Markdown and a revision token. `save`
accepts a complete Markdown document on stdin and requires `--revision TOKEN`.
`rename SOURCE DESTINATION --revision TOKEN` moves without changing frontmatter;
changing a slug is a separate metadata edit. `archive PATH --revision TOKEN`
moves to `.archive/` and prints the recovery path. Restore with ordinary tools
after checking the destination. No hard-delete command exists.

```python
from burzblog import ContentStore, Document

store = ContentStore("demo-content")
store.create("posts/example.md", Document.new("Example", "example", "# Example\n"))
snapshot = store.read("posts/example.md")
snapshot.document.body += "\nMore text.\n"
store.save("posts/example.md", snapshot.document, snapshot.revision)
```

All reads consult disk. No-op saves preserve bytes and modification time.
Unknown supported YAML fields remain intact. Paths, ownership, links, file sizes
and revisions are checked. These checks have explicit limits: see
[security](SECURITY.md), [architecture](docs/architecture/foundation.md), and
[frontmatter](docs/frontmatter.md). Saves and moves retain captured originals for
[recovery](docs/recovery.md), including late external writes. Conflicts require
review; they are never automatically merged. Metadata edits can normalize YAML formatting.

## Scope and operation

No authentication, HTTP API, rendered preview, visual editor, file watcher,
private remote credential storage yet. [Git inspection and explicit operations](docs/git.md)
support scoped commits, branches, fetch, recovery-aware fast-forward updates and
non-forced push. Ordinary Git remains available. No browser interface exists yet.
The core neither executes Markdown nor fetches embedded URLs. Explicit Git
fetch/push may contact an operator-selected remote; setup/build download dependencies. Telemetry, analytics and
tracking are absent.

Back up the entire content directory, including `.archive` and `.git` if used.
No startup migration runs. No database backup is needed in this phase.
Local CLI use is the only supported interface; do not expose it through an
unauthenticated network wrapper. A [Docker packaging setup](docs/containers.md)
includes local builds, persistent content mounts and a future image update path.
No image has been published or runtime-tested yet. See [self-hosting](docs/self-hosting.md).

## Contributing and license

See [CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md),
and [dependency review](docs/dependencies.md).

**License pending maintainer decision. No open-source license has been granted
yet.** This is intended to become an open-source project; public visibility is
not itself a license. No code from the previous BurzBlog was imported.
