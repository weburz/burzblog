# Connecting your website

BurzBlog is where posts are written. Your website stays a separate project,
built and hosted wherever you like. While it builds, it downloads the published
posts from BurzBlog as Markdown files.

This keeps the site static and independent: if BurzBlog is down, the site keeps
serving its last build; only new posts wait.

## 1. Create a build token

In the panel, open **Settings → Your website → New build token**. Copy the token
(it starts with `bzb_` and is shown once) and store it as a secret environment
variable in your host, for example `BURZBLOG_TOKEN` in Vercel.

A build token can only read published posts. It cannot read drafts, change
anything or sign in. Revoke it in Settings at any time.

## 2. Download posts in the build

Either with curl and tar, which every build image has:

```sh
mkdir -p content/blogs
curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \
  https://blog.example.com/api/export.tar.gz | tar -xz -C content/blogs
```

Or with the `burzblog` binary, which also removes files of posts that are no
longer published:

```sh
BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog export --out content/blogs --clean
```

The archive contains one `<slug>.md` per published post and a `redirects.json`
with the old addresses of renamed posts (see
[Redirecting renamed posts](#redirecting-renamed-posts)). When BurzBlog knows
your website's address it also holds `feed.xml`, `feed.atom`, `feed.json` and
`sitemap.xml` at the top level; see [Feeds and sitemap](#feeds-and-sitemap).
`burzblog export` writes them all next to the posts, and `--clean` leaves them
alone.

## The file format

```markdown
---
title: "Web Authentication Methods Explained: Pros, Cons, and Best Practices"
description: A guide to authentication in web applications.
status: published
tags:
  - authentication
  - web-security
author: Ada Lovelace
seo:
  title: Web authentication methods compared
  description: Passwords, tokens, sessions and passkeys, and when to use each.
canonical: https://example.com/blogs/a-comprehensive-guide-authentication-in-web-applications
timestamps:
  publishedOn: 2026-04-25T01:46:48Z
  updatedOn: 2026-05-01T10:00:00Z
coverImage:
  url: https://images.example.com/cover.svg
  alt: A comprehensive guide to authentication in web applications.
sitemap:
  loc: /blogs/a-comprehensive-guide-authentication-in-web-applications
  changefreq: yearly
---

The post body in Markdown.
```

- `timestamps.publishedOn` is set the first time a post is published and can be
  changed in **Post settings**. `updatedOn` is only present when the author sets
  it.
- `coverImage` is present only when a cover URL is set.
- `tags` (a YAML list), `author`, `seo` and `canonical` are present only when
  set. Tags are lowercase words joined by hyphens, in the author's order; they
  hold categories too. `seo` has `title` and `description`, and any other keys
  the author added under `seo` (`robots`, ...) are written back inside it.
  `canonical` is an absolute `https://` address. Posts written before BurzBlog
  modelled these keys, which kept them as extra frontmatter, have them moved
  into these fields when the database is upgraded, and files you import or push
  with them are read the same way.
- Every other frontmatter key the author added (here `sitemap`) is passed
  through unchanged, so site-specific fields keep working. Extra keys may not
  use merge keys (`<<`) or YAML tags other than the core ones (`!!str`,
  `!!int`, `!!float`, `!!bool`, `!!null`, `!!timestamp`, `!!map`, `!!seq`,
  `!!binary`); BurzBlog refuses them on save because strict readers such as
  js-yaml would fail or resolve them into other fields.
- Values that YAML readers could misread (`yes`, `on`, a bare date) are quoted.

## Example: Nuxt Content on Vercel

For example, a Nuxt Content site that reads posts from a GitHub repository:

```ts
// content.config.ts (before)
const source = { repository: "https://github.com/example/blogposts", include: "blogs/**.md" };
```

Point it at a local folder instead:

```ts
// content.config.ts (after)
const source = process.env.NODE_ENV === "test" ? undefined : { include: "blogs/**.md" };
```

Download posts before generating, in `package.json`:

```json
{
  "scripts": {
    "posts": "mkdir -p content/blogs && curl -fsSL -H \"Authorization: Bearer $BURZBLOG_TOKEN\" \"$BURZBLOG_SERVER/api/export.tar.gz\" | tar -xz -C content/blogs",
    "generate": "pnpm posts && nuxt generate"
  }
}
```

Set `BURZBLOG_SERVER` and `BURZBLOG_TOKEN` in the Vercel project settings, and
add `content/blogs/` to `.gitignore`. The schema (`title`, `description`,
`status`, `timestamps`, `coverImage`, `sitemap`) already matches what BurzBlog
exports; add `tags: z.array(z.string()).default([])`, `author`, `canonical` and
`seo: z.object({ title, description })` if your site uses them.

## Redirecting renamed posts

When you change a published post's slug, BurzBlog remembers the old one.
`redirects.json` in the export (and `GET /api/content/redirects`) lists them:

```json
{ "items": [{ "from": "old-slug", "to": "new-slug" }] }
```

`to` is always the post's current slug, and only published posts are listed.
Your site decides what an address looks like, so map `from` and `to` onto it. In
a Nuxt site, load the file into `routeRules` so each old address answers 301:

```ts
// nuxt.config.ts
import { existsSync, readFileSync } from "node:fs";

const file = "content/blogs/redirects.json";
const moved: { from: string; to: string }[] = existsSync(file)
  ? JSON.parse(readFileSync(file, "utf8")).items
  : [];

export default defineNuxtConfig({
  routeRules: Object.fromEntries(
    moved.map(({ from, to }) => [
      `/blog/${from}`,
      { redirect: { to: `/blog/${to}`, statusCode: 301 } },
    ]),
  ),
});
```

`routeRules` are read when the site builds, and BurzBlog rebuilds it on every
save of a published post (see below), so a rename is live after the next build.
For a site that renders on a server and fetches posts at request time, ask the
content API instead: `GET /api/content/posts/old-slug` answers `308` with the
current slug, so a server middleware only needs to follow it:

```ts
// server/middleware/redirects.ts
export default defineEventHandler(async (event) => {
  const match = event.path.match(/^\/blog\/([a-z0-9-]+)$/);
  if (!match) return;
  const res = await fetch(`${process.env.BURZBLOG_SERVER}/api/content/posts/${match[1]}`, {
    headers: { Authorization: `Bearer ${process.env.BURZBLOG_TOKEN}` },
    redirect: "manual",
  });
  if (res.status !== 308) return;
  const { redirect } = await res.json();
  return sendRedirect(event, `/blog/${redirect}`, 301);
});
```

If you export into `content/blogs`, keep `redirects.json` out of Nuxt Content's
`include` pattern (the example above only matches `*.md`). Remove a redirect in
the panel, under the post's slug in its settings, when you no longer want it.

## 3. Rebuild on publish

Create a deploy hook in your host (Vercel: **Project → Settings → Git → Deploy
Hooks**) and give BurzBlog its URL through `BURZBLOG_DEPLOY_HOOK`. See
[self-hosting](self-hosting.md#2-install-the-service).

BurzBlog then POSTs to the hook a few seconds after published content changes:
publishing, unpublishing, archiving a published post, or saving changes to a
published post. Several saves in a row cause one rebuild. **Settings → Your
website** shows the last request and has **Rebuild now**.

The hook URL is treated as a secret: it is never shown in the panel or the API,
and failures are reported without it.

### Scheduled posts

A draft can be scheduled to publish itself later (**Publish → Schedule…** in
the panel, or `burzblog schedule`). A scheduled post is still a draft: it is
not in the export, and **your website does not show it until its next build**.
When the time comes BurzBlog publishes the post and calls the deploy hook like
a manual publish, so with a hook configured the site rebuilds a few seconds
later. Without a hook, the post appears at your next build. Scheduling itself
does not trigger a rebuild. If BurzBlog was stopped at the scheduled time, the
post publishes when it starts again.

## Feeds and sitemap

BurzBlog makes an RSS feed, an Atom feed, a JSON Feed and a sitemap for your
website to publish. Readers never contact BurzBlog, so the site copies the files
at build time and serves them itself. Tell BurzBlog where the site lives:

```sh
BURZBLOG_SITE_URL=https://jarmos.dev
BURZBLOG_SITE_TITLE="Jarmos"
BURZBLOG_SITE_DESCRIPTION="Notes on software"
BURZBLOG_POST_PATH=/blog/{slug}      # the default
BURZBLOG_TAG_PATH=/blog/tags/{tag}   # optional: tag pages in the sitemap
```

Post links are `https://jarmos.dev/blog/<slug>`, so `--post-path` must match the
route your site uses (the default fits a Nuxt Content layout with
`content/blogs/<slug>.md` served at `/blog/<slug>`). Then either take the files
from the archive you already download:

```sh
curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \
  "$BURZBLOG_SERVER/api/export.tar.gz" | tar -xz -C content/blogs
mv content/blogs/feed.* content/blogs/sitemap.xml public/
```

or fetch only what you need, which also lets you ask for a tag feed or more
posts (`?limit=` up to 100, `?tag=`):

```sh
for f in feed.xml feed.atom feed.json sitemap.xml; do
  curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" "$BURZBLOG_SERVER/api/content/$f" -o "public/$f"
done
```

Files in `public/` are served at `/feed.xml` and `/sitemap.xml` after
`nuxt generate`. Reference them from your pages with
`<link rel="alternate" type="application/rss+xml" href="/feed.xml">` and in
`robots.txt` as `Sitemap: https://jarmos.dev/sitemap.xml`. Without a site URL
the routes answer `409 site_url_missing` and the archive has no such files.
Feed items hold each post as sanitized HTML; links inside a post are written as
the author wrote them, so use absolute links in posts that readers will open
from a feed reader. **Settings → Your website** shows whether the site URL is set.

## Previews and images

The panel's preview renders Markdown without raw HTML, like most static site
generators' safe defaults, and loads images from their `https://` URLs. Your
site's own renderer (for example Nuxt Content with MDC components) decides the
final look; components the preview does not know appear as plain text there.
