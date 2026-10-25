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

The archive contains one `<slug>.md` per published post and nothing else.

## The file format

```markdown
---
title: "Web Authentication Methods Explained: Pros, Cons, and Best Practices"
description: A guide to authentication in web applications.
status: published
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
exports.

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

## Previews and images

The panel's preview renders Markdown without raw HTML, like most static site
generators' safe defaults, and loads images from their `https://` URLs. Your
site's own renderer (for example Nuxt Content with MDC components) decides the
final look; components the preview does not know appear as plain text there.
