# Dependency decisions

## Browser application

Nuxt 4/Vue 3/Nuxt UI 4/Tailwind 4 and Oxlint/Oxfmt follow the inspected UniBurz
toolchain. Versions are exact in `web/package.json` and transitive packages locked
with pnpm 11. No Pinia or rich-text editor dependency. Framework/tools are MIT;
Playwright is Apache-2.0, bundled Sofia Sans/JetBrains Mono fonts are OFL-1.1 and
Lucide icons are ISC. Assets remain package-managed, not copied from old products.
Family palette values are adapted from UniBurz's design tokens, not business UI.

The installed development/build tree is substantial (Nuxt UI's transitive tools),
while deployment uses only the generated static files in `.output/public`, not
`node_modules/` or any Node.js runtime: the Python process serves them (ADR-011)
and the container image contains no Node.js at all. Font discovery and
remote icon fetching are disabled; installed fonts/icons are bundled locally.

Audit on 2026-10-02 found two transitive advisories. A narrow `fontless>esbuild`
override uses 0.28.1 for [GHSA-g7r4-m6w7-qqqr](https://github.com/advisories/GHSA-g7r4-m6w7-qqqr).
[GHSA-86w9-cpqp-85rv](https://github.com/advisories/GHSA-86w9-cpqp-85rv) affects
node-forge <=1.4.0; no patched release was available from the registry (despite
the audit client suggesting 1.4.1). It enters through listhen's development TLS
tooling. BurzBlog does not use its RSA signature verification, serve development
TLS, run Node.js in production, or ship node-forge/listhen in `.output`. The production build gate rejects
these packages in deployable output. Production TLS belongs to the operator's
reverse proxy. This is a bounded reachability assessment, **not a clean dependency
audit or a claim that the upstream bug is fixed**. Keep the advisory visible and
update when an upstream patch is available; do not add audit suppression.

## Authenticated application additions

- FastAPI (MIT), Uvicorn (BSD-3-Clause): typed HTTP boundary and ASGI server,
  avoiding a hand-built HTTP parser/router. No standard-extra bundle is required.
- argon2-cffi (MIT): maintained Argon2id hashing instead of custom password crypto.
- markdown-it-py (MIT), nh3 (MIT): rendering with raw HTML disabled plus a separate
  sanitizer. Rendering alone is not considered sanitization.
- httpx (BSD-3-Clause), development only: API tests through Starlette TestClient.
  Current Starlette emits a deprecation warning recommending httpx2; the existing
  client remains functional, and no migration is claimed here.

Versions/transitive packages are locked by uv. These require ongoing advisory
review, not a claim of absence of defects. References:
[FastAPI](https://fastapi.tiangolo.com/),
[argon2-cffi](https://argon2-cffi.readthedocs.io/),
[Markdown security](https://markdown-it-py.readthedocs.io/en/latest/security.html),
[nh3](https://github.com/messense/nh3).

## Original foundation

No previous implementation code is reused. Tooling references were inspected
only for conventions (uv, Ruff, typing, pytest and Conventional Commits).

Runtime: `ruamel.yaml` 0.18.16 (MIT). It provides YAML 1.2 round-trip parsing,
preserving comments, key order and quotes better than PyYAML's ordinary dump.
It has a long-lived upstream and a deliberately pinned pre-1.0 API. Its size and
parser surface are justified by frontmatter compatibility; a custom parser is
not. The pure-Python round-trip loader is used. Its optional platform C helper
may be present in the resolved environment, but is not used by this parser.

Sources: [upstream overview](https://yaml.dev/doc/ruamel.yaml/overview/),
[API stability guidance](https://yaml.dev/doc/ruamel.yaml/api/),
[package metadata](https://pypi.org/project/ruamel.yaml/0.18.16/).
These explain capability and provenance, not a claim of absence of vulnerabilities.
No independent security audit has been performed. Review advisories and lockfile
changes before release; resource limits and restricted YAML reduce exposure.

Development/build: Ruff (MIT), mypy (MIT), Pyright (MIT), pre-commit (MIT),
pre-commit-hooks (MIT), pytest (MIT), pytest-cov (MIT), Hatchling (MIT),
and their locked transitive dependencies. Pyright uses Node only for development.
Task orchestrates commands; Crisp checks commit messages; Hadolint checks Dockerfiles.
No frontend bundle
or runtime web framework is installed. Redistribution must retain dependency
license notices; the maintainer's eventual project license must be checked for
compatibility. The lockfile records resolved versions and artifact hashes.
