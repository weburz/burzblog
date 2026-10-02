"""Content application services shared by HTTP routes without owning file formats."""

from dataclasses import dataclass

from burzblog.document import Document, parse, serialize
from burzblog.errors import ConflictError, ContentError
from burzblog.storage import ContentStore


@dataclass(frozen=True)
class Article:
    """A revision-aware document view; Markdown remains the durable representation."""

    path: str
    revision: str
    markdown: str
    title: str
    status: str


class ContentApplication:
    """Orchestrate list/read/create/save/rename/archive through the content store."""

    def __init__(self, store: ContentStore) -> None:
        """Bind services to one operator-configured storage boundary."""
        self.store = store

    def read(self, path: str) -> Article:
        """Read fresh disk state without derived content persistence."""
        snapshot = self.store.read(path)
        metadata = snapshot.document.metadata
        return Article(
            path,
            snapshot.revision,
            serialize(snapshot.document),
            metadata.get("title", path.removesuffix(".md")),
            metadata.get("status", "draft"),
        )

    def list(self, offset: int = 0, limit: int = 100) -> dict[str, object]:
        """Return bounded summaries, surfacing malformed files without hiding other posts."""
        paths = self.store.discover()
        items: list[dict[str, str]] = []
        for path in paths[offset : offset + limit]:
            try:
                article = self.read(path)
                items.append(
                    {"path": path, "title": article.title, "status": article.status}
                )
            except (ContentError, OSError):
                items.append({"path": path, "title": path, "status": "unavailable"})
        return {"items": items, "total": len(paths)}

    def create(self, title: str, slug: str, body: str) -> Article:
        """Create a draft in the configured posts collection without overwriting."""
        document = Document.new(title, slug, body)
        path = f"posts/{slug}.md"
        revision = self.store.create(path, document)
        return self._verified_read(path, revision)

    def save(self, path: str, markdown: str, revision: str) -> Article:
        """Save exact Markdown only against the caller's observed revision."""
        written = self.store.save(path, parse(markdown), revision)
        return self._verified_read(path, written)

    def publish(self, path: str, revision: str) -> Article:
        """Publish portable metadata only; no implicit deployment or Git push."""
        snapshot = self.store.read(path)
        snapshot.document.metadata["status"] = "published"
        written = self.store.save(path, snapshot.document, revision)
        return self._verified_read(path, written)

    def _verified_read(self, path: str, revision: str) -> Article:
        """Do not report another writer's immediate replacement as our saved result."""
        article = self.read(path)
        if article.revision != revision:
            raise ConflictError(
                "File changed again after publication; retain editor input."
            )
        return article

    def rename(self, path: str, destination: str, revision: str) -> Article:
        """Move without overwriting, preserving metadata as supplied in the file."""
        self.store.rename(path, destination, revision)
        return self.read(destination)

    def archive(self, path: str, revision: str) -> str:
        """Move into the reversible archive, retaining recovery copies."""
        return self.store.archive(path, revision)
