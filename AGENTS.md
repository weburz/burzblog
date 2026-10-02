# BurzBlog

Phase 1 is a Python 3.12+ POSIX library and local CLI. Markdown files are
canonical; there is no database, web server, renderer, or Git integration yet.
Read `docs/architecture/foundation.md` and `SECURITY.md` before storage changes.

- `task setup`: install locked dependency groups using uv.
- `task dev`: show the CLI help.
- `task check`: Ruff, strict mypy/Pyright, tests, build and repository hygiene.
- `task format`: apply Ruff formatting. Make commands remain compatibility aliases.
- `task hooks:install`: optional pre-commit and Crisp commit-message hooks.
- Follow UniBurz tooling: uv, Ruff, mypy, Pyright, pre-commit and Crisp.
  Future frontend tooling uses pnpm, Oxlint and Oxfmt; do not add Node tools yet.
- Keep domain parsing independent of filesystem and application interfaces.
- Preserve original bytes on no-op saves and Markdown bodies on metadata edits.
- Never follow symlinks or overwrite a destination during create/rename.
- Test filesystem behavior in temporary directories, including failure paths.
- Do not copy the previous implementation or select a project license.
- Follow the authorized milestone order: recovery, Git, application/auth, then UI.
- Every changing save/move retains its source inode under `.burzblog-recovery`.
  Never prune journals automatically or include them in application Git commits.
- Python uses normal `def` syntax; the global arrow-function rule applies to JS/TS.
- Use a short-lived feature branch; no Plane issue exists for this bootstrap.
  Commit via Crisp when available. Never push directly to main.
- GitHub Actions is intentionally disabled by the maintainer. Do not enable it.
  Run `task check` locally and record exact results in PRs before merging.
  Keep the dormant workflow reproducible for future use.
- Docker: `task docker:lint` (Hadolint), `task docker:build`, `task docker:smoke`.
  Docker runtime checks cannot run in the sandbox; report them as unrun.
  Keep content outside images and preserve host UID/GID; see `docs/containers.md`.

The pre-existing `admin-panel/` cache is unrelated and must not be incorporated.
