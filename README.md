# BurzBlog

A self-hosted, file-first Markdown CMS for a single editor. Write in your browser,
keep articles as ordinary files, and deliver published content to your own website.
Git is optional; your files remain the source of truth.

BurzBlog runs independently of your reader website. Its application image, your
content directory, and its private SQLite login state are separate, so replacing
the application does not replace your articles. The editor's Publish button marks
an article for publication; deployment is an explicit separate step.

**Release status:** preparing the first 1.0.0 release. No public image is available
yet; build from source using the [container guide](docs/containers.md). See
[launch readiness](docs/launch.md) for the remaining verification gates.

## From writing to a website

1. Install the editor with the [container guide](docs/containers.md) or
   [non-container setup](docs/self-hosting.md).
2. Store your articles in a separate content directory, optionally backed by a
   private content-only Git repository. Keep application code out of that repo.
3. Write and mark articles published; explicitly commit and push if using Git.
4. Export a stable copy with `burzblog --root /path/to/content export`, build your
   reader, and deploy only its public output.

The [publication guide](docs/publishing.md) defines the validated JSON contract
and includes a working static reader example. Drafts and unknown frontmatter
fields are excluded. Invalid exports stop the build before any JSON is emitted.

Media uploads and automatic website deployment are not included. Use hosted
HTTPS image/video URLs; private preview omits images. BurzBlog supplies an editor,
file recovery, optional Git operations, and a publication boundary—not website
themes, hosting, or multi-user collaboration.

```text
Browser / CLI → authenticated application → ordinary Markdown ←→ external editors
                                                       ↕
                                                  Git → remote
```

If BurzBlog disappears, nothing needs exporting: keep using your Markdown files
and ordinary Git repository.

## Development

Prerequisites: Python 3.12+, [uv](https://docs.astral.sh/uv/),
[Task](https://taskfile.dev/), Node 24 LTS, pnpm 11, Git, and a local POSIX
filesystem. Docker is optional. The Python runtime needs neither Node.js nor any
external service. The optional HTTP application uses local SQLite authentication
state; the content library and CLI need no database or credentials.

```sh
task setup
task dev
task check
```

`uv.lock` locks dependencies. `task check` runs formatting, lint, strict typing,
unit/integration/CLI tests, package build, frontend checks and browser E2E.
`task docker:check` additionally lints, builds and acceptance-tests the container image (24 checks) where a Docker daemon exists.
See [browser setup and usage](docs/browser.md) to configure paths and initialize
the administrator before `task dev`. GitHub Actions is intentionally disabled;
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
[security boundaries](docs/security.md), [architecture](docs/architecture/foundation.md), and
[frontmatter](docs/frontmatter.md). Saves and moves retain captured originals for
[recovery](docs/recovery.md), including late external writes. Conflicts require
review; they are never automatically merged. Metadata edits can normalize YAML formatting.

## Scope and operation

The [browser](docs/browser.md) and [authenticated API](docs/application.md) support
listing, revision-aware source editing, archival, publication metadata and safe
preview. No visual editor or file watcher yet.
[Git inspection and explicit operations](docs/git.md)
support scoped commits, branches, fetch, recovery-aware fast-forward updates and
non-forced push, including private HTTPS repositories authenticated with an
operator-held token file. Ordinary Git remains available.
The core neither executes Markdown nor fetches embedded URLs. Explicit Git
fetch/push may contact an operator-selected remote; setup/build download dependencies. Telemetry, analytics and
tracking are absent.

See the [measured local baseline](docs/performance.md) and
[focused security review](docs/security-review.md) for evidence and limitations.

Back up the entire content directory, including `.archive` and `.git` if used.
No content migration runs at startup. API installations must also back up private
authentication state separately. Do not expose the CLI through an
unauthenticated network wrapper.

## Deploy

One non-root process serves the interface and API; content, login state and Git
secrets stay in host directories. The [container guide](docs/containers.md) covers
building the image, initialization, an HTTPS reverse proxy (validated Caddy and
nginx examples in `deploy/`), private Git remotes, [backups](docs/backup.md),
upgrades and rollback; [self-hosting](docs/self-hosting.md) also covers a
non-container setup. `task docker:check` verifies the image; it has passed on native
Docker Engine (Debian 12) and Docker Desktop. The first image and GitHub release follow [docs/release.md](docs/release.md) after validation on the intended production host (issue #5); until then build the image locally.

## Contributing and license

See [CONTRIBUTING.md](CONTRIBUTING.md) and [dependency review](docs/dependencies.md).
BurzBlog is licensed under the [Apache License 2.0](LICENSE). No code from the
previous BurzBlog was imported.
