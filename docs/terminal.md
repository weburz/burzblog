# Writing from the terminal

The `burzblog` binary is also the client. Posts always live on the server; files
on your computer are working copies.

## Install

On Linux and macOS, one line installs the latest release:

```sh
curl -fsSL https://burzblog.weburz.com/install.sh | sh
```

It picks the build for your computer, checks it against the release's
`SHA256SUMS`, and installs it to `/usr/local/bin` if that is writable,
otherwise `~/.local/bin` (it never runs `sudo` for you). Set
`BURZBLOG_VERSION=v2.0.0` to pin a version or `BURZBLOG_INSTALL_DIR` to choose
the directory. Prefer to check what you run? Read the script first:
`curl -fsSL https://burzblog.weburz.com/install.sh | less`. The script only
works once the repository and its releases are public. It downloads over HTTPS
only. The checksum guards against a corrupted download, not against a
compromised release: releases are not signed yet, so `SHA256SUMS` comes from
the same place as the binary.

To install by hand instead, `burzblog` is one file with nothing else to
install. Download the build for your computer from the
[latest release](https://github.com/weburz/burzblog/releases/latest):

| Computer | File |
|---|---|
| Linux (Intel/AMD) | `burzblog-<version>-linux-amd64` |
| Linux (ARM 64-bit), Raspberry Pi 4/5 | `burzblog-<version>-linux-arm64` |
| Raspberry Pi (32-bit OS) | `burzblog-<version>-linux-arm` |
| macOS (Apple silicon) | `burzblog-<version>-darwin-arm64` |
| macOS (Intel) | `burzblog-<version>-darwin-amd64` |
| Windows | `burzblog-<version>-windows-amd64.exe` |

On Linux and macOS, make it executable and put it on your `PATH`:

```sh
chmod +x burzblog-*-linux-amd64
sudo mv burzblog-*-linux-amd64 /usr/local/bin/burzblog
burzblog --version
```

macOS may refuse to open a downloaded binary the first time; run
`xattr -d com.apple.quarantine /usr/local/bin/burzblog` once. Check the file
against `SHA256SUMS-<version>` from the same release with `sha256sum -c`
(`shasum -a 256 -c` on macOS).

With Go installed you can build it instead: clone the repository and run
`task build` (the binary lands in `bin/burzblog`). The same binary also runs the
server (`burzblog serve`), so a server host already has it.

## Connect once

```sh
burzblog login https://blog.example.com      # asks for username and password
burzblog whoami
```

`login` creates an API token named after your computer and stores it in
`~/.config/burzblog/config.json` (mode `0600`). It appears in the panel under
**Settings → Connected computers**, where it can be revoked. `burzblog logout`
revokes it and forgets the server. Plain `http://` is refused except for
`localhost`, so your password never travels unencrypted. The client does not
follow redirects either; if the server answers with one, `login` stops and
tells you the address to use, for example `burzblog login https://blog.example.com`.

## Everyday commands

```sh
burzblog list                          # newest first; --status draft|scheduled|published|archived, --search text,
                                       # --sort updated|published|title|created, --tag name (`ls` works too)
burzblog new "Understanding Go interfaces"   # creates a draft and opens it in your editor
                                       # (only in a terminal; --no-edit skips it, --slug sets the URL name)
burzblog edit understanding-go-interfaces
burzblog publish understanding-go-interfaces
burzblog unpublish | archive | restore <slug>
burzblog schedule <slug> "2026-11-03 09:00"   # publish a draft by itself, at a future time
burzblog unschedule <slug>
burzblog open [slug]                   # open the browser panel
```

`schedule` takes an RFC 3339 time (`2026-11-03T09:00:00Z`) or a local date and
time in quotes (`"2026-11-03 09:00"`, in this computer's time zone). The time
must be in the future and at most two years away. `list` marks scheduled drafts
with their time, and `list --status scheduled` shows only those. Publishing,
archiving or unscheduling a post ends its schedule. A scheduled draft is
published as it is at the due time, including any edits made after it was
scheduled, and it still publishes if the session or token that scheduled it has
since been revoked. Unschedule it to stop that.

`edit` downloads the post, opens it in `$VISUAL` or `$EDITOR` (then `vi`), and
saves it back when the editor exits. Editors that return immediately need their
wait flag, for example `EDITOR="code --wait"`.

The file looks like this:

```markdown
---
title: Understanding Go interfaces
description: Small interfaces, accepted where they are used.
status: draft
tags:
  - go
  - api-design
author: Ada Lovelace
seo:
  title: Go interfaces explained
  description: Why small interfaces make Go code easier to change.
canonical: https://example.com/blog/go-interfaces
timestamps:
  publishedOn: 2026-10-24T09:30:00Z
coverImage:
  url: https://images.example.com/cover.png
  alt: A diagram of three small interfaces.
sitemap:
  changefreq: yearly
---

The post body in Markdown.
```

- `title` is required. `status` is `draft` or `published`; changing it to
  `published` publishes the post when you save.
- The slug is the post's name on the server and the file name, so BurzBlog
  does not write it into the frontmatter. A `slug` key you add must match.
- `tags` is a list of lowercase words with hyphens (`Machine Learning` is saved
  as `machine-learning`), at most 20 tags of 40 characters each, in your order;
  it is also where categories go. `tags: go, web` (one comma-separated line)
  is accepted too. `author` is a display name; `burzblog new` fills it with
  your account name. `seo.title` and `seo.description` are for search engines
  and social cards, and `canonical` is an absolute `https://` address. All of
  them are optional and left out of the file when empty. A file that leaves
  out `author` clears the post's author when you push it.
- Any other keys (`sitemap`, ...) are kept exactly as written and passed
  through to your website, and so are other keys under `seo` (`robots`, ...).

## Your own tools: pull and push

```sh
burzblog pull my-post                  # writes ./my-post.md
burzblog pull my-post -o -             # prints it
burzblog push my-post.md               # saves it back
burzblog push new-idea.md              # creates "new-idea" if it does not exist
burzblog push - --slug my-post --base-version 3   # reads the file from standard input
```

The file name is the slug, so name new files `<slug>.md` with lowercase letters,
digits and hyphens. `pull -o` also needs a file name ending in `<slug>.md` (it
may be in another folder), or `-`.

## Comparing with the server: diff and status

```sh
burzblog diff my-post.md               # server's version -> your file
burzblog diff my-post                  # the same, for ./my-post.md
burzblog diff my-post drafts/copy.md   # a file with another name
burzblog status                        # every <slug>.md in this folder
burzblog status drafts
```

`diff` prints a unified diff from the server's current post, as the Markdown
with frontmatter that `pull` writes, to your file. Lines starting with `-` are
only on the server, lines starting with `+` only in your file. It is colored
under the same rules as the rest of the CLI: no color when output is piped or
`NO_COLOR` is set. On standard error it says whether the file is based on the
server's current version or an older one, using the version `pull` or your last
`push` recorded for that file. A file this computer never pulled or pushed has
no known base version.

The exit code follows `diff(1)`: `0` identical, `1` different. Failures use the
usual codes: `4` the post is not on the server, `5` not signed in, `2` an
unreadable file or a bad name. A network or server failure is also `1`, so a
script that must tell it apart from "different" should use `--json`: it
writes an error document to standard error only for failures.

Text output is for reading: tabs become spaces, a line ending in a carriage
return shows `␍`, and control characters from the post are replaced by `�`.
`--json` prints one document with the exact, unsanitised diff:
`{"slug","baseVersion","serverVersion","stale","identical","diff"}`.
`baseVersion` is `null` when unknown, and `stale` is `true` when the server's
version is newer than `baseVersion`. Lines are compared exactly, so a missing
final newline or Windows line endings show up as differences. Two very large,
very different files are shown as one block removed and one added instead of a
minimal diff.

`status` looks at each `.md` file in the folder (default `.`; subfolders are
not entered) whose name is a valid slug, and prints one line per file:

| State | Meaning |
|---|---|
| `up to date` | the file matches the server |
| `modified locally` | you edited the file; the server has not moved on |
| `changed on server` | the server's version is newer than the one you pulled; your file is untouched |
| `both changed` | both of the above; run `burzblog diff <file>` before pushing |
| `not on server` | no post has that slug yet; `push` would create it |
| `differs, not pulled by this computer` | the file differs and this computer has no record of where it came from |

With `--json` it prints
`{"dir","files":[{"file","slug","state","baseVersion","serverVersion"}]}`, where
`state` is `up_to_date`, `modified`, `changed_on_server`, `both_changed`,
`not_on_server` or `not_pulled`. The exit code is non-zero only when something
went wrong: not signed in, an unreadable folder or file, a network failure.

## When someone else saved first

Every save says which version it started from. If the post changed on the
server in the meantime, nothing is overwritten:

- `edit` keeps your text in a draft file and tells you where it is. Running
  `burzblog edit <slug>` again offers to continue with it.
- `push` refuses. Compare with `burzblog diff <file>`, then either edit
  your file to include their changes or `burzblog push <file> --force` to
  replace their version with yours.

Replaced versions are never lost: the panel's **History** keeps every save and
can restore any of them.

If a save is refused because something in the file is invalid (for example an
empty title or broken YAML), `edit` shows the reason and offers to reopen the
editor; your text is kept either way. Control characters (tabs, escape
sequences) are not allowed in `title`, `description` or the cover `alt`; the
error names the field. Anything the server sends is shown with such characters
replaced by `�`.

## Importing existing posts

```sh
burzblog import content/blog           # every .md file in the folder
burzblog import content/blog --recursive
burzblog import a.md b.md --update
burzblog import content/blog --dry-run
```

The inverse of `export`: bring posts from a static site repository, for example
a Nuxt Content `content/blog/` folder of `<slug>.md` files, into BurzBlog. The
slug is the frontmatter `slug`, or the file name without `.md`. The title,
description, `tags`, `author`, `seo`, `canonical`, `timestamps`, `coverImage`
and any other frontmatter keys are kept. A file with `status: published` is imported as published; a file without
a status becomes a draft. `--draft` imports everything as a draft.
If the frontmatter `slug` differs from the file name, a warning is printed (and
included as `"warning"` in `--json`), and with `--update` the file is refused so
it cannot overwrite a post its name does not hint at; rename the file or fix the
slug.

- Only `.md` files are read. Folders are not entered unless you add `--recursive`.
- A post that already exists is never overwritten: it prints
  `skipped my-post: already exists`. With `--update` the file is saved over it as
  a new version, so the old text stays in **History**. Together with `--draft`
  that also turns a published post back into a draft.
- `--dry-run` prints what would happen and changes nothing.
- One bad file (broken YAML, an invalid slug, no title) does not stop the
  others. It is listed on standard error and the exit code is non-zero: `6`
  when every failure was invalid content, `1` otherwise.
- With `--json`, standard output holds one document:
  `{"dryRun","created","updated","unchanged","skipped","failed","files":[{"file","slug","action","status","version","error"}]}`.
  `action` is `created`, `updated`, `unchanged`, `skipped` or `failed`; `error`
  is `{"code","message"}` and only present on failures.

## Backing up your posts

```sh
burzblog backup                        # ./burzblog-backup-<date>/
burzblog backup ~/backups/blog
```

Writes every post, drafts and archived ones too, to
`<dir>/<status>/<slug>.md` with a `README.txt`. Restore a file with
`burzblog push <file>.md`. History is not included; the database file is the
complete backup (see [self-hosting](self-hosting.md#backups)). The panel offers
the same download under **Settings → Backup**.

## Renamed posts: redirects

```sh
burzblog redirects            # FROM and TO, one line per old slug
burzblog redirects --json     # {"items":[{"from":"old-slug","to":"new-slug"}]}
```

Lists the old slugs of renamed published posts and the slug each has now, the
same list a website gets from `GET /api/content/redirects`. `burzblog export`
also writes it to `redirects.json` in the output folder. Rename a post by
changing its slug in the panel's post settings; that is also where a redirect is
removed.

## Build machines

For reader-site builds, use a build token from **Settings → Your website**
instead of logging in:

```sh
BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog export --out content/blogs --clean
```

Build tokens can only read published posts. See [publishing](publishing.md).

## Scripts and AI agents

Every command works without a person: nothing prompts or opens an editor unless
standard input is a terminal. Results can be printed as JSON, failures have
their own exit codes, and a save always names the version it started from, so a
script cannot silently overwrite someone else's work.

### Sign in without a terminal

Write tokens come from `burzblog login`; the panel's **Settings** page only
creates build (export) tokens. Sign in once on a computer you control, ideally
with an account used only by the agent, then hand the stored token to the
script. It appears under **Settings → Connected computers**, where it can be
revoked on its own:

```sh
burzblog login https://blog.example.com -u agent      # once, asks for the password
python3 -c 'import json,os; print(json.load(open(os.path.expanduser("~/.config/burzblog/config.json")))["token"])'

export BURZBLOG_SERVER=https://blog.example.com
export BURZBLOG_TOKEN=bzb_...        # a write token; build tokens only work for `export` and the content API
burzblog whoami --json
```

The environment wins over the stored login. `burzblog login` itself can also
run unattended: `echo "$PASSWORD" | burzblog login https://blog.example.com -u ada --password-stdin`.

### JSON output

`--json` is a global flag, so it works the same on every command and always
means one JSON document on standard output (one line). It is supported by
`list`, `whoami`, `new`, `pull`, `push`, `diff`, `status`, `publish`,
`unpublish`, `archive`, `restore`, `schedule`, `unschedule`, `backup`,
`import` and `redirects`. Other commands ignore it and print their usual text, but their
failures are still JSON. `new` never opens an editor with `--json`.

| Command | Output |
|---|---|
| `list` | `{"items":[{"slug","title","description","status","words","tags","author","publishedOn","publishAt","updatedAt","updatedBy","version"}],"counts":{"draft","published","archived","scheduled"}}`; `items` is `[]` when nothing matches |
| `whoami` | `{"username","server"}` |
| `new`, `publish`, `unpublish`, `archive`, `restore`, `schedule`, `unschedule` | the post: `{"slug","title","description","status","tags","author","publishedOn","publishAt","updatedAt","updatedBy","version"}` |
| `push` | the same, plus `"created"` (`true` if the post did not exist) |
| `pull` | `{"slug","version","path"}`; with `-o -` it is `{"slug","version","markdown"}` instead of the raw file |
| `diff` | `{"slug","baseVersion","serverVersion","stale","identical","diff"}`; exit `1` when different |
| `status` | `{"dir","files":[{"file","slug","state","baseVersion","serverVersion"}]}` |
| `backup` | `{"dir","posts"}` |
| `redirects` | `{"items":[{"from","to"}]}`; `items` is `[]` when there are none |

The field names are the HTTP API's (see [api.md](api.md#posts)); bodies are
never included. `publishedOn` is `null` until a post is published, and
`publishAt` is `null` unless the draft is scheduled.

```sh
$ burzblog list --status draft --json
{"items":[{"slug":"release-notes","title":"Release notes","description":"","status":"draft","words":212,"tags":["release"],"author":"agent","publishedOn":null,"publishAt":null,"updatedAt":"2026-10-25T09:12:00Z","updatedBy":"agent","version":3}],"counts":{"archived":0,"draft":1,"published":4,"scheduled":0}}

$ burzblog new "Release notes" --body-file notes.md --json
{"slug":"release-notes","title":"Release notes","description":"","status":"draft","tags":[],"author":"agent","publishedOn":null,"publishAt":null,"updatedAt":"2026-10-25T09:12:00Z","updatedBy":"agent","version":2}
```

With `--json`, errors go to standard error as one line, with a non-zero exit
code, and standard output stays empty:

```json
{"error":{"code":"conflict","message":"release-notes changed on the server since you pulled it, so nothing was overwritten.","detail":"Compare with `burzblog pull release-notes -o -`, then push again with --force to replace it"}}
```

`code` is stable and meant for programs (`usage`, `conflict`, `not_found`,
`authentication`, `validation`, `error`, or one of the [API's error
codes](api.md#errors) such as `slug_taken`); `message` and `detail` are for
people and may change.

### Writing without an editor

```sh
burzblog new "Release notes" --no-edit                  # creates the draft, prints "Created draft release-notes."
burzblog new "Release notes" --body-file notes.md       # the body (Markdown, no frontmatter) from a file
generate-notes | burzblog new "Release notes" --body-file - --json
burzblog pull my-post -o - | burzblog push - --slug my-post --base-version 3
```

`new` opens an editor only when standard input is a terminal and neither
`--no-edit`, `--body-file` nor `--json` is given. `edit` needs a terminal or
`$VISUAL`/`$EDITOR`; scripts should use `pull` and `push`. `login` and
`user add` read their password from standard input when it is not a terminal,
and `edit`'s questions answer themselves with their default.

`push -` needs `--slug` and, for a post that already exists, `--base-version`
(the version you started from; `list --json` and `pull --json` show it). Without
it `push -` refuses to touch an existing post, because nothing records where
the text came from. `push <file>` remembers the version from `pull`.

### A safe edit loop for an agent

```sh
set -eu
slug=release-notes

# 1. pull; the version the text started from is remembered for this file
mkdir -p work
burzblog pull "$slug" -o "work/$slug.md" --force --json

# 2. modify work/$slug.md with any tool

# 3. push it back. The server compares against the pulled version.
status=0
burzblog push "work/$slug.md" --json || status=$?
case $status in
  0) ;;
  3) echo "someone saved first: pull again, redo the change, retry" ;;  # never add --force
  6) echo "the file is invalid: fix it, retry" ;;
  *) echo "failed with exit code $status" ;;
esac
```

Rules that keep this safe:

- Always push the file you pulled, or pass `--base-version`. A conflict (exit
  3) means nothing was written. Pull again, redo the change on the new text and
  push again. Do not use `--force` in automation; it replaces the other version.
- Prefer `--json` and branch on the exit code, not on message text.
- Create drafts, not published posts: `status: published` in a file publishes
  on save. Publishing is `burzblog publish <slug>`, a separate step.

Mistakes are recoverable: every save keeps the previous content as a revision,
including a `--force` push, and the panel's **History** restores any of them.
Nothing here deletes a post (`delete` is panel-only and only for archived posts).

### Exit codes

| Code | Meaning | JSON `error.code` |
|---|---|---|
| `0` | success | |
| `1` | any other failure: network, server error, unreadable file, ...; also `diff` finding a difference (no error document) | `error`, or the API's code |
| `2` | wrong flags or arguments, or a file or `--slug` that is not a valid slug | `usage` |
| `3` | version conflict (the post changed since its base version) or the slug is already taken | `conflict`, `slug_taken` |
| `4` | the post does not exist | `not_found` |
| `5` | not signed in, or the token or password is not accepted | `authentication`, `credentials` |
| `6` | the server rejected the content (empty title, broken YAML, ...) | `validation` |

Scripts that only test for zero versus non-zero keep working. Failures that
used to exit `1` and now exit `2` to `6` are conflicts, missing posts, sign-in
problems, invalid content and wrong usage.
