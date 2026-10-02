# BurzBlog

Phase 1 is a Python 3.12+ POSIX library and local CLI. Markdown files are
canonical; Git operations and the authenticated API are separate from storage.
Read `docs/architecture/foundation.md` and `SECURITY.md` before storage changes.

- `task setup`: install locked dependency groups using uv.
- `task dev`: configured API and Nuxt development servers; see docs/application.md.
- `task check`: backend gates plus frontend format/lint/types/tests/build/E2E.
- `task backend:check`: independent Python checks; `task web:check`: browser gates.
- `task measure`: disposable 1,000-file local performance fixture; not a capacity claim.
- Browser E2E uses disposable /tmp fixtures and port 3000 (the production
  single-process topology); never reuse a running real instance. Tests must not point at operator content.
- `task format`: apply Ruff formatting. Make commands remain compatibility aliases.
- `task hooks:install`: optional pre-commit and Crisp commit-message hooks.
- Follow UniBurz tooling: uv, Ruff, mypy, Pyright, pre-commit and Crisp.
  Frontend in web/ uses pinned pnpm 11, Nuxt UI, Oxlint and Oxfmt.
- Keep domain parsing independent of filesystem and application interfaces.
- Preserve original bytes on no-op saves and Markdown bodies on metadata edits.
- Never follow symlinks or overwrite a destination during create/rename.
- Test filesystem behavior in temporary directories, including failure paths.
- Do not copy the previous implementation or select a project license.
- Follow the authorized milestone order: recovery, Git, application/auth, then UI.
- Every changing save/move retains its source inode under `.burzblog-recovery`.
  Never prune journals automatically or include them in application Git commits.
- Git lives in `git_domain.py`/`git_process.py`; read ADR-006 before changing it.
  Never inherit arbitrary Git environment/configuration or expose raw stderr/URLs.
- Git mutations live in `git_mutations.py`; read ADR-007. Never use Git checkout
  to overwrite content. A transition marker blocks further Git writes on failure.
- Python uses normal `def` syntax; the global arrow-function rule applies to JS/TS.
- API/auth: read ADR-008 and docs/application.md. SQLite stays outside content/Git.
  Never add an unauthenticated content route. Async API tests may need unsandboxed
  wakeup sockets; do not skip them at merge.
- Use a short-lived feature branch; no Plane issue exists for this bootstrap.
  Commit via Crisp when available. Never push directly to main.
- GitHub Actions is intentionally disabled by the maintainer. Do not enable it.
  Run `task check` locally and record exact results in PRs before merging.
  Keep the dormant workflow reproducible for future use.
- Docker: `task docker:check` (Hadolint, build, `scripts/docker-smoke.py`). Run it
  where a Docker daemon exists; otherwise report the runtime checks as unrun.
  Keep content/state/secrets outside images and preserve host UID/GID; see
  `docs/containers.md` and ADR-011.

The pre-existing `admin-panel/` cache is unrelated and must not be incorporated.
