"""Content application services shared by HTTP routes without owning file formats."""

from dataclasses import dataclass
from datetime import UTC, datetime
from threading import Lock
from typing import Literal

from burzblog.document import Document, parse, serialize, unread_opening
from burzblog.errors import ConflictError, ContentError, public_message
from burzblog.storage import ContentStore, Listed

UNREAD_OPENING = (
    "This file opens with frontmatter BurzBlog cannot read (a byte-order mark, "
    "blank lines before ---, spaces after --- or TOML +++). Fix the opening lines; "
    "until then it is treated as body text and cannot be published."
)


@dataclass(frozen=True)
class Article:
    """A revision-aware document view; Markdown remains the durable representation."""

    path: str
    revision: str
    markdown: str
    title: str
    status: str
    # Why the frontmatter cannot be read; `markdown` is then the file's exact text.
    problem: str | None = None


class ContentApplication:
    """Orchestrate list/read/create/save/rename/archive through the content store."""

    def __init__(self, store: ContentStore) -> None:
        """Bind services to one operator-configured storage boundary."""
        self.store = store
        # Process memory only, never persisted: list summaries of readable files,
        # reused while a file's stat signature is unchanged. Reads for editing
        # always go to disk.
        self._summaries: dict[
            str, tuple[tuple[int, int, int, int], dict[str, str]]
        ] = {}
        # Requests run in a thread pool; the cache is shared between them.
        self._lock = Lock()

    def read(self, path: str) -> Article:
        """Read fresh disk state without derived content persistence."""
        snapshot = self.store.read(path)
        return self._article(path, snapshot.revision, snapshot.document)

    def editable(self, path: str) -> Article:
        """Read for editing, including a file whose frontmatter cannot be parsed.

        Such a file comes back as its exact text with the reason, so it can be
        repaired: saving parses the new text and keeps the usual revision check
        and recovery copy. Unsafe, oversized or non-UTF-8 files still fail.
        """
        text, revision = self.store.read_text(path)
        try:
            document = parse(text)
        except ContentError as error:
            problem = public_message(error) or "The frontmatter cannot be read."
            return Article(path, revision, text, "", "unavailable", problem)
        if not document.metadata and unread_opening(text):
            return Article(path, revision, text, "", "unavailable", UNREAD_OPENING)
        return self._article(path, revision, document)

    @staticmethod
    def _article(path: str, revision: str, document: Document) -> Article:
        """Present a parsed document; its Markdown round-trips byte for byte."""
        metadata = document.metadata
        return Article(
            path,
            revision,
            serialize(document),
            # No invented title: the browser shows an untitled file by its filename.
            metadata.get("title", ""),
            metadata.get("status", "draft"),
        )

    def summary(self, path: str, modified: int) -> dict[str, str]:
        """Describe one listed file; an unreadable file is surfaced, not hidden."""
        changed = datetime.fromtimestamp(modified / 1e9, UTC).isoformat(
            timespec="seconds"
        )
        try:
            article = self.read(path)
        except (ContentError, OSError):
            return {
                "path": path,
                "title": path,
                "status": "unavailable",
                "modified": changed,
            }
        return {
            "path": path,
            "title": article.title,
            "status": article.status,
            "modified": changed,
        }

    def _summarized(self, entry: Listed) -> dict[str, str]:
        """Reuse the summary of an unchanged readable file; read anything else."""
        with self._lock:
            cached = self._summaries.get(entry.path)
        if cached is not None and cached[0] == entry.signature:
            return cached[1]
        summary = self.summary(entry.path, entry.modified)
        if summary["status"] != "unavailable":
            with self._lock:
                self._summaries[entry.path] = (entry.signature, summary)
        return summary

    def list(
        self,
        offset: int = 0,
        limit: int = 100,
        status: str | None = None,
        query: str = "",
        order: Literal["path", "modified"] = "path",
    ) -> dict[str, object]:
        """Return bounded summaries of every file, or of every file that matches.

        Without a filter only the requested page is read. A status or text filter
        needs every file's summary; unchanged files reuse the in-memory summary,
        so only new or changed files are parsed again.
        """
        survey = self.store.survey()
        entries = survey.entries
        left_out = {
            "unsupported_names": survey.unsupported_names,
            "unsafe_folders": survey.unsafe_folders,
        }
        if order == "modified":
            entries.sort(key=lambda entry: (-entry.modified, entry.path))
        needle = query.strip().casefold()
        if status is None and not needle:
            page = entries[offset : offset + limit]
            return {
                "items": [self._summarized(entry) for entry in page],
                "total": len(entries),
                "files": len(entries),
                "left_out": left_out,
            }
        present = {entry.path for entry in entries}
        with self._lock:
            for path in [path for path in self._summaries if path not in present]:
                del self._summaries[path]
        matches = [
            summary
            for summary in map(self._summarized, entries)
            if (status is None or summary["status"] == status)
            and (
                needle in summary["title"].casefold()
                or needle in summary["path"].casefold()
            )
        ]
        return {
            "items": matches[offset : offset + limit],
            "total": len(matches),
            "files": len(entries),
            "left_out": left_out,
        }

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
        return self._set_status(path, revision, "published")

    def unpublish(self, path: str, revision: str) -> Article:
        """Return a post to draft; the body and every other field stay as they are."""
        return self._set_status(path, revision, "draft")

    def _set_status(self, path: str, revision: str, status: str) -> Article:
        """Change only `status`, through the same revision check and recovery."""
        snapshot = self.store.read(path)
        source = snapshot.document.source or ""
        if not snapshot.document.metadata and unread_opening(source):
            raise ContentError(UNREAD_OPENING)
        snapshot.document.metadata["status"] = status
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
        # A file moved while its frontmatter is broken still moved successfully.
        return self.editable(destination)

    def archive(self, path: str, revision: str) -> str:
        """Move into the reversible archive, retaining recovery copies."""
        return self.store.archive(path, revision)
