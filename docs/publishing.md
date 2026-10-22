# Publishing content to a website

BurzBlog is a self-hosted editorial CMS. It stores Markdown, provides an
authenticated editor, and optionally transports content with Git. Your reader
site is deployed separately and never needs access to the editor's login state.

The editor's **Publish** action sets `status: published`. It does not commit,
push, deploy, or confirm that a website is live. Those are explicit steps. A
failed deployment must leave the previous public release in place.

## Separate application, content, and public output

- Install BurzBlog from its application image or source release. Do not put your
  articles in a checkout of the BurzBlog application repository.
- Keep canonical Markdown in a separate private content directory. Optionally
  use a dedicated private content repository and a token limited to that repo.
- Keep authentication state and remote credentials outside content and Git.
- Give the reader build read access to a stable copy of content, not the live
  editor directory. A disposable checkout of a specific content commit works.
- Deploy only reader output. Never serve the content checkout, `.git`, drafts,
  `.archive`, or `.burzblog-recovery` as static assets.

Git is optional transport, not a backup of all recovery data and not a deployment
engine. With Git, commit the selected Markdown files and push explicitly from
the Repository view. Keep build scripts and deployment credentials in the reader
application repository, not the content repository. Content must never be
evaluated as templates, MDX, shell commands, or executable configuration.

## Export the public contract

Run against a quiescent copy, with files owned by the build user and without
group/world write permissions:

```sh
uv run --frozen burzblog --root /absolute/content-checkout export > /tmp/publication.next.json
```

The command exits nonzero without JSON output if validation fails. **Check the
exit status before using the file.** Shell redirection truncates its destination
even on failure; never redirect to the currently deployed artifact. Output is
bounded to 64 MiB and contains no source paths or original frontmatter.

Schema version 1:

```json
{
  "schema_version": 1,
  "articles": [
    {
      "title": "Hello",
      "slug": "hello",
      "description": "A first article",
      "authors": ["Editor"],
      "tags": ["News"],
      "date": "2026-10-22",
      "body": "Hello, readers!\n"
    }
  ]
}
```

- Only exact `status: published` is eligible. Drafts and missing status are
  excluded. Every discovered Markdown file is parsed; malformed drafts also
  block the export rather than guessing their status.
- Published articles must have valid title and slug; duplicate published slugs
  fail the whole export. Articles are sorted by slug for reproducible output.
- Required article fields are `title`, `slug`, and `body`. Optional public fields
  are `date`, `updated`, `authors`, `tags`, `description`, and `image`. Dates are
  ISO strings. Other frontmatter, including comments and private editorial notes,
  is omitted. To publish a custom field, define a reviewed contract extension.
- The body is verbatim Markdown, **not sanitized HTML**. Editorial secrets in a
  published body or an allowed public field will be public. The exporter cannot
  infer whether text was intended to be private.
- Hidden entries and symlinks are not discovered. Unsupported Markdown names
  and unsafe folders reported by discovery block export. Visible unreadable,
  unsafe, or oversized Markdown also fails. Discovery retains its documented
  depth and entry limits.
- An empty publication fails unless `--allow-empty` is explicitly supplied.
  Use that flag deliberately when withdrawing the last article.
- A second inventory detects ordinary concurrent changes. This is not an atomic
  filesystem snapshot; do not export from a directory being edited or updated.

Consumers must reject unknown schema versions, sanitize rendered Markdown, escape
metadata in HTML, and accept only appropriate URL schemes. Treat export/build
errors as failed releases. Removing an article from the next complete release
must remove its old public page too; copying over an old directory leaves stale
articles behind. Purge public caches when withdrawing previously published text.

## Try the reference reader

[`examples/reader/build.py`](../examples/reader/build.py) is an intentionally
minimal static reader, with an index and one page per article. It uses the
existing locked Markdown renderer and sanitizer, escapes titles, disables raw
HTML, and allows HTTPS images through its page CSP. No JavaScript, templates,
remote fetches, or new dependencies are involved. It does not provide search,
themes, SEO metadata, or video embeds; customize a reader for those features.

From a BurzBlog source checkout, using a new destination outside the content root:

```sh
set -eu
release_dir="$(mktemp -d /tmp/burzblog-reader.XXXXXX)"
uv run --frozen burzblog --root /absolute/content-checkout export > "$release_dir/articles.json"
uv run --frozen python examples/reader/build.py "$release_dir/site" < "$release_dir/articles.json"
uv run --frozen python -m http.server 8081 --bind 127.0.0.1 --directory "$release_dir/site"
```

Open `http://127.0.0.1:8081`. This local server is for checking the output. Deploy
the **site directory only** through your hosting provider's release mechanism,
after the build succeeds. The reader refuses an existing destination; a failed
build may leave a partial new directory, which must be discarded. Keep the
previous deployed release available for rollback. In automated builds, apply
time and memory limits as you would for any Markdown processing job.

## Images and videos

This release does not upload, store, resize, or commit media. Host media on your
own asset service or object storage and use absolute HTTPS URLs in Markdown:

```markdown
![A descriptive alternative text](https://media.example.org/photo.jpg)

[Watch the video](https://media.example.org/video.mp4)
```

The export preserves these references and the optional `image` metadata field;
it does not fetch them or verify availability. The reference reader displays
inline Markdown images, but does not render `image` as a cover. Videos are links,
not embeds. BurzBlog's private editor preview continues to omit images to avoid
tracking requests. Readers loading external media contact that media host.

## Release acceptance

Before using a reader in production, verify a published post and hosted image,
a draft remaining absent, a duplicate slug failing the build, an unpublish
removing the old page, and a failed build retaining the previous live release.
Test the actual hosting provider's promotion and cache behavior separately.
`tests/integration/test_publication.py` covers the local export/reader boundary;
it is not evidence of a deployment on your host.
