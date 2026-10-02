"""Conservative Markdown preview with no raw HTML, images or remote fetches."""

import nh3
from markdown_it import MarkdownIt

from burzblog.document import parse


def render_preview(markdown: str) -> str:
    """Render and sanitize body text; strip images to prevent tracking requests."""
    body = parse(markdown).body
    rendered = MarkdownIt("commonmark", {"html": False}).enable("table").render(body)
    return nh3.clean(
        rendered,
        tags={
            "p",
            "br",
            "hr",
            "h1",
            "h2",
            "h3",
            "h4",
            "h5",
            "h6",
            "strong",
            "em",
            "s",
            "blockquote",
            "pre",
            "code",
            "ul",
            "ol",
            "li",
            "a",
            "table",
            "thead",
            "tbody",
            "tr",
            "th",
            "td",
        },
        attributes={"a": {"href", "title"}},
        url_schemes={"https", "http", "mailto"},
        link_rel="noopener noreferrer",
    )
