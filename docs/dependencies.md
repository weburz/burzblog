# Dependency decisions

## Browser application

Nuxt 4/Vue 3/Nuxt UI 4/Tailwind 4 and Oxlint/Oxfmt follow the inspected UniBurz
toolchain. Versions are exact in `web/package.json` and transitive packages locked
with pnpm 11. No Pinia or rich-text editor dependency. Framework/tools are MIT;
Playwright is Apache-2.0, bundled Sofia Sans/JetBrains Mono fonts are OFL-1.1 and
Lucide icons are ISC. Assets remain package-managed, not copied from old products.
Family palette values are adapted from UniBurz's design tokens, not business UI.
All of these are compatible with redistribution under the project's Apache-2.0
license, provided their notices are retained.

The installed development/build tree is substantial (Nuxt UI's transitive tools),
while deployment uses only the generated static files in `.output/public`, not
`node_modules/` or any Node.js runtime: the Python process serves them (ADR-011)
and the container image contains no Node.js at all. Font discovery and
remote icon fetching are disabled; installed fonts/icons are bundled locally.

Playwright is held at 1.61.1 (Chromium 149) for browser acceptance tests.
Playwright 1.62.1's Chromium 151 does not deliver `requestAnimationFrame`
callbacks on the supported WSL2 development host, even on an otherwise empty
page. Because Playwright uses animation frames to establish that a control is
stable before clicking it, every pointer interaction then waits until the test
timeout; the sign-in button was simply the first click in the suite. Chromium
149 delivers frames normally and the complete desktop, tablet and mobile flows
pass without forced clicks. Re-test the empty-page frame probe and the full E2E
suite before lifting this pin.

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

Re-audit on 2026-10-06 (`pnpm audit --prod`): six advisories, all in build- or development-time tooling that never ships. node-forge (GHSA-86w9-cpqp-85rv, issue #11) is unchanged; the registry still has no release after 1.4.0. braces ≤3.0.3 (GHSA-vfj7-8cjw-p6xm) is reached only through nitropack's build-time globbing. simple-git ≤3.36.0 / 4.0.0 and @simple-git/argv-parser <2.0.1 (GHSA-v5rq-49vh-5v5c, GHSA-x6jw-m9v5-85vh, GHSA-g4wm-2vf7-vfgr, GHSA-858h-whjf-mvg5) come through `@nuxt/devtools`, which is disabled in `nuxt.config.ts` and is not loaded by `nuxt generate`. None of these packages exists in `.output/public`, which is the only thing the Python process serves and the only thing copied into the image; `web/scripts/check-runtime.mjs` now refuses them as well. Patched versions exist for braces and simple-git, so a narrow pnpm override is possible when the next dependency update is made; it is not applied here to keep the release lockfile unchanged. `pip-audit` over `uv export --no-dev` on the same date reports no known vulnerabilities in the Python runtime set.

## Launch audit

The initial launch audit on 2026-10-22 (`pnpm audit --prod --json`) exited 1 with
15 advisories: 2 critical, 6 high, 6 moderate, and 1 low. In addition to the
previously recorded packages, Vite 8.3.1 and launch-editor 2.14.1 have development
server/editor advisories. These are not Python runtime dependencies. The
supported production topology still serves generated static assets without a
Node.js server; that boundary does not make the development dependency audit
clean. Auditing development dependencies additionally identified two critical
tinypool findings and two moderate Vitest mocker findings.

Launch preparation pins compatible fixes: Vite 8.3.3, launch-editor 2.14.2,
tinypool 2.1.2, and Vitest 4.1.12. Transitive pins live in
`web/pnpm-workspace.yaml`; pnpm regenerates the lockfile. TypeScript, Vue,
vue-tsc, and Playwright versions are unchanged. No audit suppression is added.

The final full `pnpm audit --json` still exits 1: six advisories, two critical
and four high, in node-forge, braces, simple-git and its argv-parser. Registry
queries for node-forge 1.4.1, braces 3.0.4, and simple-git 3.36.1 returned no
matching version. A trial simple-git 4.0.1 override failed the default-export
API used by Nuxt DevTools (`Git(root)` was not callable), so it was removed.
DevTools remains disabled; a compatible upstream integration is required rather
than forcing that major version. These findings are tracked in
[issue #61](https://github.com/weburz/burzblog/issues/61) and existing issue #11.
This is an improved tooling tree, not a clean security audit.

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
license notices; the project license is Apache-2.0, with which every locked runtime and
build dependency (MIT, BSD-3-Clause, Apache-2.0, ISC, OFL-1.1) is compatible. The lockfile records resolved versions and artifact hashes.
