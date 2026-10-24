# Contributing

Read `AGENTS.md` and `docs/adr/013-database-storage.md` first. Use `task setup`,
`task dev` and `task check`; `task format` formats Go and the panel.

Go code follows the Google Go style guide, is formatted with gofmt and handles
errors explicitly with messages that tell people what to do. Tests use the
standard `testing` package with positive and negative cases; anything touching
the database or files uses temporary directories. The panel uses Nuxt UI, arrow
functions, Vitest for units and Playwright for end-to-end tests against the real
binary.

Use short-lived feature branches and Conventional Commit subjects validated with
`crisp message 'feat(store): describe the change'`. Open a PR with the exact
verification commands and results, and any limitations. Do not bypass reviews.
Actions is intentionally disabled; record local verification before merging.
The dormant check workflow uses read permissions and no repository secrets.

Review dependency updates deliberately: `go get` then `go mod tidy` for Go,
pnpm for the panel. Never hand-edit `go.sum` or the pnpm lockfile.

Store migrations are append-only: add a new entry to `migrations` in
`internal/store/store.go`, never edit one that has shipped.

No code should be copied from other projects without a provenance and license
review. Contributions are accepted under the repository's Apache-2.0 license
(`LICENSE`); there is no CLA and no sign-off requirement.
