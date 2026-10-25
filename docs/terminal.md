# Writing from the terminal

The `burzblog` binary is also the client. Posts always live on the server; files
on your computer are working copies.

## Connect once

```sh
burzblog login https://blog.example.com      # asks for username and password
burzblog whoami
```

`login` creates an API token named after your computer and stores it in
`~/.config/burzblog/config.json` (mode `0600`). It appears in the panel under
**Settings → Connected computers**, where it can be revoked. `burzblog logout`
revokes it and forgets the server. Plain `http://` is refused except for
`localhost`, so your password never travels unencrypted.

## Everyday commands

```sh
burzblog list                          # newest first; --status draft|published|archived, --search text,
                                       # --sort updated|published|title|created
burzblog new "Understanding Go interfaces"   # creates a draft and opens it in your editor
burzblog edit understanding-go-interfaces
burzblog publish understanding-go-interfaces
burzblog unpublish | archive | restore <slug>
burzblog open [slug]                   # open the browser panel
```

`edit` downloads the post, opens it in `$VISUAL` or `$EDITOR` (then `vi`), and
saves it back when the editor exits. Editors that return immediately need their
wait flag, for example `EDITOR="code --wait"`.

The file looks like this:

```markdown
---
title: Understanding Go interfaces
description: Small interfaces, accepted where they are used.
status: draft
timestamps:
  publishedOn: 2026-10-24T09:30:00Z
coverImage:
  url: https://ik.imagekit.io/you/cover.png
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
- Any other keys (`sitemap`, `seo`, ...) are kept exactly as written and passed
  through to your website.

## Your own tools: pull and push

```sh
burzblog pull my-post                  # writes ./my-post.md
burzblog pull my-post -o -             # prints it
burzblog push my-post.md               # saves it back
burzblog push new-idea.md              # creates "new-idea" if it does not exist
```

The file name is the slug, so name new files `<slug>.md` with lowercase letters,
digits and hyphens.

## When someone else saved first

Every save says which version it started from. If the post changed on the
server in the meantime, nothing is overwritten:

- `edit` keeps your text in a draft file and tells you where it is. Running
  `burzblog edit <slug>` again offers to continue with it.
- `push` refuses. Compare with `burzblog pull <slug> -o -`, then either edit
  your file to include their changes or `burzblog push <file> --force` to
  replace their version with yours.

Replaced versions are never lost: the panel's **History** keeps every save and
can restore any of them.

If a save is refused because something in the file is invalid (for example an
empty title or broken YAML), `edit` shows the reason and offers to reopen the
editor; your text is kept either way.

## Build machines

For reader-site builds, use a build token from **Settings → Build tokens**
instead of logging in:

```sh
BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog export --out content/blogs --clean
```

Build tokens can only download published posts. See [publishing](publishing.md).
