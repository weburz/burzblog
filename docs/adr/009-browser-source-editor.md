# ADR-009: Markdown browser editor with explicit saves

Status: accepted for the first browser workflow.

## Decision

Use the inspected UniBurz conventions: Nuxt 4, Vue 3, Nuxt UI 4, Tailwind 4,
pnpm 11, Oxlint/Oxfmt, Vitest/Vue Test Utils and Playwright. Use Sofia Sans and
JetBrains Mono bundled locally. BurzYellow/BurzBlue palette values follow the
family's theme; no ecommerce components or old CMS implementation were imported.

The authenticated UI is client-rendered: no public article pages or SEO surface
justify server-rendering private editor state. The small Nuxt Node server serves
assets and proxies a fixed loopback API endpoint. A production HTTPS proxy sits
in front. This adds a Node process alongside Python; static hosting optimization
can wait until the workflow is stable. No Pinia or editor-document dependency.

Use a native source textarea and explicit Save. The same full Markdown document,
including frontmatter, is sent to the revision-aware API. A no-op returns original
bytes even when textarea input normalizes CRLF. Actual source edits use browser
line endings; no fidelity guarantee is made for visual editing because there is
no visual editor. Unknown frontmatter stays in source.

Failed saves retain input in memory with persistent error text. Check disk never
replaces dirty input automatically. An external version can be reviewed alongside
local input; replacement requires an explicit decision. There is no autosave,
offline persistence or hidden localStorage content store. Tab/browser termination
can lose unsent writing; an unload warning is best-effort only.

Preview is backend-sanitized and additionally displayed in a sandboxed iframe
with a deny-by-default content policy. Fonts/icons have no third-party runtime
fetches. Repository operations are deliberately separate from ordinary writing.
Publish updates frontmatter only; commit/push remain explicit.

## Verification and limits

Production-build browser tests use local real Markdown files and a bare remote,
at desktop/tablet/mobile viewport sizes. Unit tests cover request and editor
failure semantics. Source preservation, external conflicts, network failure,
XSS and path rejection are tested; no claim of comprehensive accessibility or
mobile-device certification is made. Native dialogs guard destructive choices.

No rich-text mode, media manager, automatic change watcher, dark mode or private
remote credentials are included. Filtering operates on the current page of 100
files. Larger indexes and automatic refresh remain future measured decisions.
