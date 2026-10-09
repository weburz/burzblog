"""Bounded frontmatter parsing and source-preserving serialization."""

import copy
import io
import math
import re
from collections.abc import Iterable
from dataclasses import dataclass
from datetime import date, datetime
from typing import Any, Protocol, TextIO, cast

from ruamel.yaml import YAML
from ruamel.yaml.events import CollectionEndEvent, CollectionStartEvent
from ruamel.yaml.tokens import AliasToken, AnchorToken, DirectiveToken, TagToken

from burzblog.errors import ContentError

MAX_BYTES = 2 * 1024 * 1024
MAX_FRONTMATTER = 64 * 1024
SLUG = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
FIELDS = (
    "title",
    "slug",
    "status",
    "date",
    "updated",
    "authors",
    "tags",
    "description",
    "image",
)


class _YamlParser(Protocol):
    """Typed boundary for the third-party parser's unannotated methods."""

    def scan(self, stream: str) -> Iterable[object]: ...
    def parse(self, stream: str) -> Iterable[object]: ...
    def load(self, stream: str) -> Any: ...
    def dump(self, data: object, stream: TextIO) -> None: ...


def _yaml() -> _YamlParser:
    """Create an isolated parser; no shared mutable parser state."""
    parser = YAML(typ="rt", pure=True)
    parser.preserve_quotes = True
    parser.allow_duplicate_keys = False
    return cast(_YamlParser, parser)


def _text(value: str) -> None:
    """Refuse lone surrogates, such as JSON-style surrogate escapes: not text."""
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        raise ContentError(
            "Frontmatter contains text that is not valid Unicode."
        ) from None


def _tree(value: Any, depth: int = 0) -> None:
    """Restrict metadata to portable scalars, lists and string-key mappings."""
    if depth > 32:
        raise ContentError("Frontmatter nesting exceeds 32 levels.")
    if isinstance(value, dict):
        for key, item in cast(dict[object, Any], value).items():
            if not isinstance(key, str):
                raise ContentError("Frontmatter keys must be strings.")
            _text(key)
            _tree(item, depth + 1)
    elif isinstance(value, list):
        items = cast(list[object], value)
        for item in items:
            _tree(item, depth + 1)
    elif isinstance(value, float):
        if not math.isfinite(value):
            raise ContentError("Non-finite numbers are unsupported.")
    elif isinstance(value, str):
        _text(value)
    elif value is not None and not isinstance(value, (bool, int, date)):
        raise ContentError("Unsupported frontmatter value.")


def validate(metadata: dict[str, Any]) -> None:
    """Validate present known fields without requiring CMS metadata externally."""
    _tree(metadata)
    for field in ("title", "slug", "status", "description", "image"):
        if field in metadata and not isinstance(metadata[field], str):
            raise ContentError(f"{field} must be a string.")
    if "title" in metadata and not metadata["title"].strip():
        raise ContentError("title must not be empty.")
    if "slug" in metadata and not SLUG.fullmatch(metadata["slug"]):
        raise ContentError("slug must contain lowercase words separated by hyphens.")
    if "status" in metadata and metadata["status"] not in ("draft", "published"):
        raise ContentError("status must be draft or published.")
    for field in ("authors", "tags"):
        if field in metadata and (
            not isinstance(metadata[field], list)
            or any(not isinstance(item, str) for item in metadata[field])
        ):
            raise ContentError(f"{field} must be an array of strings.")
    for field in ("date", "updated"):
        if field in metadata:
            value = metadata[field]
            if not isinstance(value, (str, date)):
                raise ContentError(f"{field} must be an ISO date or timestamp.")
            try:
                if isinstance(value, str):
                    datetime.fromisoformat(value)
            except ValueError:
                raise ContentError(
                    f"{field} must be an ISO date or timestamp."
                ) from None


def _flow_depth(text: str) -> int:
    """Deepest `[`/`{` nesting outside comments, in one linear pass.

    ruamel's scanner is superlinear in flow nesting (64 KiB of "[" took over a
    minute), so this cheap bound runs first. Brackets inside quoted or plain
    scalars count too: that can only make the check stricter, and tracking
    quotes would let an apostrophe in a plain scalar hide what follows.
    """
    depth = deepest = 0
    for line in text.split("\n"):
        previous = " "
        for character in line:
            if character == "#" and previous in " \t":
                break
            if character in "[{":
                depth += 1
                deepest = max(deepest, depth)
            elif character in "]}":
                depth = max(depth - 1, 0)
            previous = character
    return deepest


def _load(text: str) -> dict[str, Any]:
    """Reject expensive or application-specific YAML before constructing values."""
    if len(text.encode("utf-8")) > MAX_FRONTMATTER:
        raise ContentError("Frontmatter exceeds 64 KiB.")
    if _flow_depth(text) > 32:
        raise ContentError("Frontmatter exceeds structural limits.")
    parser = _yaml()
    try:
        for count, token in enumerate(parser.scan(text)):
            # The event limit below is 10,000; stop scanning well before a
            # pathological token stream is exhausted.
            if count > 20000:
                raise ContentError("Frontmatter exceeds structural limits.")
            if isinstance(token, (AliasToken, AnchorToken, DirectiveToken, TagToken)):
                raise ContentError(
                    "YAML aliases, anchors, directives and tags are unsupported."
                )
        depth = 0
        for count, event in enumerate(parser.parse(text)):
            if isinstance(event, CollectionStartEvent):
                depth += 1
            elif isinstance(event, CollectionEndEvent):
                depth -= 1
            if depth > 32 or count > 10000:
                raise ContentError("Frontmatter exceeds structural limits.")
        value = parser.load(text)
    except ContentError:
        raise
    except Exception:  # noqa: BLE001 - ruamel also raises TypeError, AssertionError...
        # Any parser failure is malformed content, never a server error.
        raise ContentError("Malformed or duplicate-key YAML frontmatter.") from None
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ContentError("Frontmatter must be a mapping.")
    metadata = cast(dict[str, Any], value)
    validate(metadata)
    return metadata


@dataclass
class Document:
    """Editable metadata and verbatim Markdown, with original source for no-ops."""

    metadata: dict[str, Any]
    body: str
    source: str | None = None

    @classmethod
    def new(cls, title: str, slug: str, body: str = "") -> "Document":
        """Create minimal draft metadata without implicit timestamps."""
        metadata = {"title": title, "slug": slug, "status": "draft"}
        validate(metadata)
        body = body.replace("\r\n", "\n").replace("\r", "\n")
        return cls(metadata, body.rstrip("\n") + "\n")


def parse(source: str) -> Document:
    """Read UTF-8 Markdown without altering body, delimiters or whitespace."""
    if len(source.encode("utf-8")) > MAX_BYTES:
        raise ContentError("Document exceeds 2 MiB.")
    lines = source.splitlines(keepends=True)
    if not lines or lines[0] not in ("---\n", "---\r\n"):
        return Document({}, source, source)
    for index, line in enumerate(lines[1:], 1):
        if line.rstrip("\r\n") == "---":
            metadata = _load("".join(lines[1:index]))
            return Document(metadata, "".join(lines[index + 1 :]), source)
    raise ContentError("Frontmatter has no closing delimiter.")


def _equivalent(left: Any, right: Any) -> bool:
    """Compare metadata without conflating YAML booleans, numbers or dates."""
    if isinstance(left, dict) and isinstance(right, dict):
        left_map = cast(dict[str, Any], left)
        right_map = cast(dict[str, Any], right)
        return left_map.keys() == right_map.keys() and all(
            _equivalent(left_map[key], right_map[key]) for key in left_map
        )
    if isinstance(left, list) and isinstance(right, list):
        left_list = cast(list[object], left)
        right_list = cast(list[object], right)
        return len(left_list) == len(right_list) and all(
            _equivalent(a, b) for a, b in zip(left_list, right_list, strict=True)
        )
    for scalar in (bool, str, int, float, datetime, date):
        if isinstance(left, scalar) or isinstance(right, scalar):
            return (
                isinstance(left, scalar)
                and isinstance(right, scalar)
                and str(left) == str(right)
            )
    return left is None and right is None


def serialize(document: Document) -> str:
    """Preserve no-ops exactly; reserialize YAML only when metadata changes."""
    validate(document.metadata)
    original = parse(document.source) if document.source is not None else None
    if original is not None and _equivalent(original.metadata, document.metadata):
        prefix = original.source or ""
        if original.body:
            prefix = prefix[: -len(original.body)]
        elif (
            prefix.startswith(("---\n", "---\r\n"))
            and not prefix.endswith("\n")
            and document.body
        ):
            prefix += "\r\n" if prefix.startswith("---\r\n") else "\n"
        result = prefix + document.body
    else:
        metadata = copy.deepcopy(original.metadata) if original else {}
        for key in list(metadata):
            if key not in document.metadata:
                del metadata[key]
        order = list(FIELDS) + sorted(set(document.metadata) - set(FIELDS))
        for key in order:
            if key in document.metadata:
                metadata[key] = document.metadata[key]
        stream = io.StringIO()
        try:
            _yaml().dump(metadata, stream)
        except Exception:  # noqa: BLE001 - emitter failures are unsupported content
            raise ContentError("Frontmatter cannot be written back safely.") from None
        newline = (
            "\r\n"
            if original and (original.source or "").startswith("---\r\n")
            else "\n"
        )
        frontmatter = stream.getvalue().replace("\n", newline)
        result = f"---{newline}{frontmatter}---{newline}{document.body}"
    # Enforce output size and ensure emitted metadata can be read under our policy.
    parse(result)
    return result
