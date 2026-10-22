"""Build a bounded public projection without exposing the editorial directory."""

import json
from datetime import date
from typing import Any

from burzblog.errors import ConflictError, ContentError
from burzblog.storage import ContentStore

MAX_EXPORT_BYTES = 64 * 1024 * 1024
PUBLIC_FIELDS = (
    "title",
    "slug",
    "date",
    "updated",
    "authors",
    "tags",
    "description",
    "image",
)


def export_publication(store: ContentStore, *, allow_empty: bool = False) -> str:
    """Return schema-v1 JSON only after validating the complete visible inventory.

    Only explicit published status is eligible. Unknown metadata and source
    paths are never exported. Use a quiescent checkout: the repeated survey
    detects ordinary concurrent changes but is not a filesystem transaction.
    """
    before = store.survey()
    if before.unsupported_names or before.unsafe_folders:
        raise ContentError("Publication requires a complete supported inventory.")
    articles: list[dict[str, Any]] = []
    slugs: set[str] = set()
    size = 0
    for entry in before.entries:
        document = store.read(entry.path).document
        metadata = document.metadata
        if metadata.get("status") != "published":
            continue
        if not metadata.get("title") or not metadata.get("slug"):
            raise ContentError("Published articles require title and slug.")
        slug = str(metadata["slug"])
        if slug in slugs:
            raise ContentError("Published article slugs must be unique.")
        slugs.add(slug)
        article = {
            field: value.isoformat() if isinstance(value, date) else value
            for field in PUBLIC_FIELDS
            if (value := metadata.get(field)) is not None
        }
        article["body"] = document.body
        size += len(json.dumps(article, ensure_ascii=False).encode("utf-8")) + 2
        if size > MAX_EXPORT_BYTES - 128:
            raise ContentError("Publication exceeds 64 MiB.")
        articles.append(article)
    if store.survey() != before:
        raise ConflictError(
            "Content changed during publication; retry from a stable checkout."
        )
    if not articles and not allow_empty:
        raise ContentError("Empty publication refused; use --allow-empty deliberately.")
    articles.sort(key=lambda article: article["slug"])
    return json.dumps(
        {"schema_version": 1, "articles": articles}, ensure_ascii=False, sort_keys=True
    )
