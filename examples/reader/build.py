"""Reference static reader: export on stdin, a new release directory as argv[1].

Run with the BurzBlog environment. No content code executes or URLs are fetched.
The caller promotes this new directory only after this process succeeds.
"""

import html
import json
import sys
from pathlib import Path

from burzblog.document import SLUG
from burzblog.publication import MAX_EXPORT_BYTES
from burzblog.render import render_html


def page(title: str, body: str) -> str:
    """Wrap sanitized HTML in a minimal accessible document."""
    return (
        '<!doctype html><html lang="en"><head><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width, initial-scale=1">'
        '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; '
        "img-src https:; base-uri 'none'; form-action 'none'\">"
        f"<title>{html.escape(title)}</title></head><body><main>{body}</main></body></html>"
    )


def build(destination: Path, source: bytes) -> None:
    """Validate the envelope and render into a new, never-overwritten directory."""
    if len(source) > MAX_EXPORT_BYTES:
        raise ValueError("Export exceeds 64 MiB.")
    payload = json.loads(source)
    if type(payload.get("schema_version")) is not int or payload["schema_version"] != 1:
        raise ValueError("Unsupported publication schema.")
    articles = payload["articles"]
    if not isinstance(articles, list):
        raise TypeError("Articles must be an array.")
    seen: set[str] = set()
    for article in articles:
        slug = article["slug"]
        if not isinstance(slug, str) or not SLUG.fullmatch(slug) or len(slug) > 125:
            raise ValueError("Unsupported article slug.")
        if slug in seen:
            raise ValueError("Duplicate article slug.")
        seen.add(slug)
        if not isinstance(article["title"], str) or not isinstance(
            article["body"], str
        ):
            raise TypeError("Article title and body must be text.")
    destination.mkdir(mode=0o755, parents=False, exist_ok=False)
    links: list[str] = []
    for article in articles:
        slug, title = article["slug"], article["title"]
        folder = destination / slug
        folder.mkdir()
        body = (
            '<nav><a href="../">All articles</a></nav>'
            f"<article><h1>{html.escape(title)}</h1>"
            f"{render_html(article['body'], images=True)}</article>"
        )
        (folder / "index.html").write_text(page(title, body), encoding="utf-8")
        links.append(f'<li><a href="{slug}/">{html.escape(title)}</a></li>')
    (destination / "index.html").write_text(
        page("Articles", "<h1>Articles</h1><ul>" + "".join(links) + "</ul>"),
        encoding="utf-8",
    )


if __name__ == "__main__":
    try:
        build(Path(sys.argv[1]), sys.stdin.buffer.read(MAX_EXPORT_BYTES + 1))
    except (ValueError, KeyError, TypeError, AttributeError, IndexError, OSError):
        print(
            "Reader build failed; do not promote this release directory.",
            file=sys.stderr,
        )
        sys.exit(1)
