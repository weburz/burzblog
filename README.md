# BurzBlog

[![License: Apache-2.0](https://img.shields.io/github/license/weburz/burzblog)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/weburz/burzblog)](https://github.com/weburz/burzblog/releases/latest)

A small, self-hosted place to write blog posts. Write in the browser, or in your
own editor from the terminal. Posts are Markdown, stored in one SQLite file, and
your website picks up the published ones when it builds.

BurzBlog is one Go binary. It runs the server, serves the browser panel, and is
also the command-line client. It is small enough for a Raspberry Pi under
systemd.

```text
 browser panel ─┐                         ┌─> your site's build downloads
                ├─> burzblog serve ── SQLite        published posts as .md files
 burzblog edit ─┘    (one binary)         └─> deploy hook triggers a rebuild
 (your $EDITOR)
```

## What it does

- **Write in the browser:** a post list with search and filters, a split editor
  with Markdown on the left and a live preview of the published post on the
  right, a formatting toolbar, post settings (slug, cover image, dates, extra
  frontmatter) and the full history of every save.
- **Write from the terminal:** `burzblog edit my-post` opens the post in
  `$EDITOR` as a normal Markdown file with frontmatter and saves it back when
  you close the editor.
- **Never overwrites silently:** every post has a version. A save based on an
  old version is refused in the panel and the CLI, and you choose which
  version to keep. Unsaved browser work is kept in the browser until you save.
- **Feeds your website:** reader sites download published posts as
  `<slug>.md` files with a read-only build token, in the frontmatter shape
  Nuxt Content and other static site generators read. An optional deploy hook
  (for example a Vercel deploy hook) rebuilds the site after you publish.

Not included yet: image and video uploads (use hosted image URLs), roles and
permissions (every account can do everything), and serving the public blog
itself.

## Install

Download the binary for your computer from the
[latest release](https://github.com/weburz/burzblog/releases/latest). Assets are
named `burzblog-vX.Y.Z-<os>-<arch>`, and a `SHA256SUMS` file is published
alongside them for verification. See
[Writing from the terminal](docs/terminal.md#install) for the full list and
setup steps.

## Quick start

To build from source:

```sh
task build                                   # bin/burzblog with the panel inside
bin/burzblog user add ada                 # create an account (asks for a password)
bin/burzblog serve                           # http://127.0.0.1:8080
```

Then, from any computer:

```sh
burzblog login https://blog.example.com
burzblog new "My next post"                  # creates a draft and opens $EDITOR
burzblog list
burzblog edit my-next-post
burzblog publish my-next-post
```

## Guides

- [Running BurzBlog](docs/self-hosting.md): systemd on a Raspberry Pi or any
  Linux host, HTTPS with Caddy or nginx, backups and upgrades.
- [Writing from the terminal](docs/terminal.md): every CLI command and what
  happens on conflicts, plus JSON output, exit codes and a safe edit loop for
  [scripts and AI agents](docs/terminal.md#scripts-and-ai-agents).
- [Connecting your website](docs/publishing.md): the export format, a Nuxt
  Content example and deploy hooks.
- [Containers](docs/containers.md): the optional Docker image.
- [HTTP API](docs/api.md) and [security model](docs/security.md).

## Development

Prerequisites: Go 1.27, [Task](https://taskfile.dev/), Node 24 LTS and pnpm 11.
Docker is optional.

```sh
task setup     # Go modules, pnpm dependencies, the Playwright browser
task dev       # API on :8080 and the panel dev server on :3000
task check     # Go fmt/vet/tests, panel lint/types/tests/build, browser E2E, hooks
```

`task docker:check` lints, builds and smoke-tests the container image where a
Docker daemon exists. GitHub Actions is intentionally disabled; `task check`
is the merge gate. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
