# Development tooling

UniBurz is the lint-tool reference: uv, Ruff, strict mypy and Pyright, pre-commit,
and Crisp. BurzPage also uses Task and uv-driven local type-check hooks. This
project adapts those conventions without importing their application code or
service dependencies. The package remains compatible with Python 3.12+.

`task setup` installs the lockfile. `task check` runs all local merge gates;
`task dev` shows the CLI. `make` commands remain compatibility aliases. Task
3.54.0 and uv 0.12.21 are pinned in the dormant workflows.

Dependency groups separate `linters`, `tests`, and `build`; default `dev` includes
all three. Runtime container builds exclude development tools. Ruff 0.16.3,
mypy 2.3.0, Pyright 1.1.411, and pre-commit 4.6.2 match UniBurz's Python tooling.
Pyright requires Node for its bundled checker; install Node 24 LTS for local
checks. Python-only runtime installations do not need Node.

Ruff applies standard errors plus imports, docstrings, modernization, bugbear,
security and naming checks. We do not copy irrelevant ORM, migration, Terraform,
Ansible, Docker-in-hook or frontend dependencies. Both type checkers are strict;
a small Protocol marks the third-party YAML parser's untyped API boundary.

`task hooks:install` optionally installs hooks. Hooks invoke tools through
`uv run --frozen` so local checks share the same versions and dependencies.
Generic hooks check YAML, large files, final newlines and trailing whitespace
while preserving Markdown hard line breaks. Install Crisp separately for the
commit-message hook. The stdin invocation also works in linked worktrees.

Actions is intentionally disabled. Record exact `task check` results in each
PR; do not report dormant workflows as executed. Docker checks are separate:
`task docker:check` runs `docker:lint`, `proxy:lint`, `docker:build` and `docker:smoke`. Hadolint
2.15.1 is the Dockerfile linter (standalone when installed, otherwise its
digest-pinned image). Where no Docker
daemon is available, report container runtime checks as unrun.

When frontend code is introduced, use pnpm, Oxlint and Oxfmt as in UniBurz.
Those tools are not installed for this Python-only foundation.
