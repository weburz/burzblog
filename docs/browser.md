# Writing in BurzBlog

The browser now supports login, listing files, creating drafts, source editing,
preview, publication metadata, rename, reversible archive and explicit Git
operations. Markdown remains readable and editable without BurzBlog.

## Run locally

Prerequisites: Python 3.12+, uv, Task, Node 24 LTS, pnpm 11 and local Git.
Run `task setup`; it installs locked Python/frontend dependencies and Chromium
for acceptance tests. On a minimal Linux machine, Playwright may additionally
require OS libraries (`cd web && pnpm exec playwright install --with-deps chromium`;
this may require administrator privileges). These browser tools are development
dependencies, not required to operate the installed application.

Create content collections and initialize authentication as described in
[application setup](application.md). For development use this exact browser URL:

```sh
export BURZBLOG_CONTENT_ROOT=/absolute/path/to/content
export BURZBLOG_STATE_DIR=/absolute/private/path/to/burzblog-state
export BURZBLOG_ORIGIN=http://127.0.0.1:3000
uv run --frozen burzblog-server init-admin
task dev
# Open http://127.0.0.1:3000
```

Only run `init-admin` once. The origin must match the address used in the browser.
State must be outside content and the Git repository. Existing content roots need
`posts/` for creation and `.archive/` for archive operations.

Before using ordinary `git add .`, add recovery/archive exclusions as described in
[recovery and Git](recovery.md#ordinary-git-workflows). BurzBlog's selected-file
commits exclude these directories, but external Git does not know their purpose.
BurzBlog does not silently rewrite your `.gitignore`.

`task dev` is development only: the Nuxt dev server on port 3000 proxies `/api/`
to the API on port 8000. For production, `cd web && pnpm build` generates a static
interface that the API process itself serves (`BURZBLOG_WEB_ROOT`), so there is
one loopback process and no Node.js runtime. Use the [container](containers.md)
or [self-hosting](self-hosting.md) guide, with an HTTPS reverse proxy in front.
This is an early self-hosted application, not a reviewed Internet appliance.

## Everyday flow

Sign in → New post → title and filename → Create draft → Markdown → Save.
Preview never writes content. Publish sets `status: published`; it does not deploy
a site or push Git. External tools can read/edit the same file at any time.
Refresh the list or choose Check disk in the editor to load external changes.
No file watcher or autosave is claimed.

The source editor includes frontmatter. Metadata can be edited directly there.
Unsupported frontmatter produces a clear failure rather than silently deleting
fields. A no-op preserves original bytes; deliberate textarea edits may normalize
line endings. Filename moves do not automatically change slug metadata.

When a file changes externally, your stale save is rejected. Keep the editor
open, use Check disk, review the disk version and copy/reconcile your text before
explicitly replacing it. Recovery errors require the operator's
[journal recovery procedure](recovery.md). Never repeatedly retry a recovery error.

If connectivity fails, the editor keeps unsent text in memory and reports it as
unsaved. It is not an offline application. Reloading/closing the tab can lose
unsaved input. When a request finds the session expired, a sign-in panel appears
above the editor; Sign in again is also in the Account menu. Neither closes the
editor. Credentials and article drafts are not stored in localStorage.

## Git

Repository is optional and separate from writing. Select individual Markdown
files, supply a commit message, then explicitly push. Fetch checks the configured
remote; Apply safe update performs only a safe fast-forward. Divergence produces
persistent attention text and no automatic merge. Branch switching refuses dirty
or unsupported transitions. See [Git boundaries](git.md). The panel shows the
configured remote's name and whether it uses operator credentials, never its
address or secret. "Remote unavailable" means the remote could not be reached or
refused the credentials; nothing local changed, so ask the operator to check the
[private remote setup](git.md#private-https-remotes). Automatic
publication-branch workflows are not implemented.

## Verification

`task check` runs Python and browser gates. `task backend:check` and
`task web:check` run them separately. E2E tests start the real production process
(API serving the generated interface) on port 3000 against new temporary
fixtures, including an authenticated HTTPS Git remote, never your content. Do not
run them alongside a real instance on that port. They also fail on any Content
Security Policy violation in the browser console. GitHub Actions is
disabled; the checked-in workflow is dormant and does not publish anything.

Keyboard focus, labels, native controls, responsive overflow and the critical
flows are covered; broader screen-reader/contrast auditing and real iOS/Android
testing remain follow-ups. Screenshots and traces are local test artifacts.
