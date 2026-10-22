"""Markdown to sanitized HTML; imported by the isolated preview worker process.

Kept free of BurzBlog's YAML stack so the worker starts quickly.
"""

import resource
import struct
import sys
from typing import BinaryIO

import nh3
from markdown_it import MarkdownIt
from markdown_it.rules_inline import StateInline
from markdown_it.token import Token

# Address space for one worker: ordinary documents need well under 100 MiB.
WORKER_MEMORY = 1024 * 1024 * 1024


def _join_fragments(state: StateInline) -> None:
    """Merge adjacent text tokens and recompute levels, in linear time.

    Replaces markdown-it-py's `fragments_join`, which concatenates each run of
    text tokens one prefix at a time and keeps every prefix alive: `"_a" * 40000`
    needed gigabytes. Output is identical.
    """
    level = 0
    kept: list[Token] = []
    parts: list[str] = []

    def flush() -> None:
        if len(parts) > 1:
            kept[-1].content = "".join(parts)

    for token in state.tokens:
        if token.nesting < 0:
            level -= 1
        token.level = level
        if token.nesting > 0:
            level += 1
        if token.type == "text" and kept and kept[-1].type == "text" and parts:
            parts.append(token.content)
            continue
        flush()
        kept.append(token)
        parts = [token.content] if token.type == "text" else []
    flush()
    state.tokens[:] = kept


def _renderer() -> MarkdownIt:
    """CommonMark with tables, raw HTML off, and the linear fragment join."""
    renderer = MarkdownIt("commonmark", {"html": False}).enable("table")
    renderer.inline.ruler2.at("fragments_join", _join_fragments)
    return renderer


def render_html(body: str, *, images: bool = False) -> str:
    """Render CommonMark without raw HTML, then allow only plain structural tags."""
    rendered = _renderer().render(body)
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
        }
        | ({"img"} if images else set()),
        attributes={"a": {"href", "title"}, "img": {"src", "alt", "title"}},
        url_schemes={"https", "http", "mailto"},
        link_rel="noopener noreferrer",
    )


def _read(stream: BinaryIO, size: int) -> bytes:
    """Read exactly `size` bytes, or fewer at end of input."""
    data = b""
    while len(data) < size:
        chunk = stream.read(size - len(data))
        if not chunk:
            break
        data += chunk
    return data


def serve() -> None:
    """Render length-prefixed UTF-8 bodies from stdin until it closes."""
    resource.setrlimit(resource.RLIMIT_AS, (WORKER_MEMORY, WORKER_MEMORY))
    source, sink = sys.stdin.buffer, sys.stdout.buffer
    while header := _read(source, 8):
        (size,) = struct.unpack(">Q", header)
        html = render_html(_read(source, size).decode("utf-8")).encode("utf-8")
        sink.write(struct.pack(">Q", len(html)) + html)
        sink.flush()


if __name__ == "__main__":
    serve()
