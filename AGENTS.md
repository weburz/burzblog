# BurzBlog

Phase 1 is a Python 3.12+ POSIX library and local CLI. Markdown files are
canonical; there is no database, web server, renderer, or Git integration yet.
Read `docs/architecture/foundation.md` and `SECURITY.md` before storage changes.

- `make setup`: install locked development dependencies using uv.
- `make dev`: show the CLI help.
- `make check`: format, lint, strict types, tests, and package build.
- `make format`: apply Ruff formatting.
- Keep domain parsing independent of filesystem and application interfaces.
- Preserve original bytes on no-op saves and Markdown bodies on metadata edits.
- Never follow symlinks or overwrite a destination during create/rename.
- Test filesystem behavior in temporary directories, including failure paths.
- Do not copy the previous implementation or select a project license.
- Do not add runtime services or implement the remaining V1 ahead of the foundation.
- Python uses normal `def` syntax; the global arrow-function rule applies to JS/TS.
- Use a short-lived feature branch; no Plane issue exists for this bootstrap.
  Commit via Crisp when available. Never push directly to main.
- CI is enabled for this fresh repository with restricted action permissions.
  Do not inherit the archived project's disabled-CI policy.

The pre-existing `admin-panel/` cache is unrelated and must not be incorporated.
