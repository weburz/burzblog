# Contributing

Read `AGENTS.md`, the architecture and ADRs first. Use `task setup`, `task dev`,
and `task check`; use `task format` for formatting. Python functions have type
hints and Google-style docstrings. Prefer small, stateless domain functions and
explicit filesystem failures. No unrelated frameworks or services.

Use short-lived feature branches and Conventional Commit subjects validated
with `crisp message 'feat(content): describe the change'`. Open a PR with exact
verification results and limitations. Do not bypass required checks or reviews.
Actions is intentionally disabled; record local verification before merging.
The dormant check workflow uses read permissions and no repository secrets.
The optional image publisher grants package write access only to its release job.
Review dependency
updates deliberately and regenerate `uv.lock` with uv; never hand-edit it.

Tests under `tests/unit` use no filesystem; `tests/integration` use temporary
directories and real local files/processes. Add regression tests for failures
and preservation, not just successful CRUD. Browser E2E runs Playwright against the production topology (`task web:check`); unit tests in `web/tests/unit` use Vitest.

No code should be copied from other projects without a provenance and license
review. Contributions are accepted under the repository's Apache-2.0 license
(`LICENSE`); there is no CLA and no sign-off requirement.
