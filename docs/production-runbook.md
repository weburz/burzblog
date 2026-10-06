# Production-host validation runbook

Checklist for the intended production server, run once before the first release.
Details live in [containers.md](containers.md) and [backup.md](backup.md). Run as
the ordinary account that owns the content, never with `sudo`.

1. Directories: `install -d -m 0700 state secrets; mkdir -p site/posts site/.archive; chmod go-w site`.
2. Ownership: `id -u` and `id -g` into `BURZBLOG_UID`/`BURZBLOG_GID` in `.env`.
3. Secrets: put the Git token file (mode 0600) in `secrets/`; add the CA file if the remote uses a private CA.
4. Content mount: set `BURZBLOG_SITE_DIR` and, if Markdown is in a subfolder, `BURZBLOG_CONTENT_SUBDIR`.
5. State mount: confirm `state` is private (0700) and outside content and Git.
6. Git token: set `BURZBLOG_REMOTE_URL`, `BURZBLOG_REMOTE_USERNAME`, `BURZBLOG_REMOTE_TOKEN_FILE` (and `BURZBLOG_REMOTE_CA_FILE`) per [containers.md](containers.md#private-git-remote).
7. Origin: set `BURZBLOG_ORIGIN` to the exact HTTPS origin people type.
8. Image: `task docker:build` from the release commit; later set the published digest in `BURZBLOG_IMAGE`.
9. Administrator: `docker compose run --rm burzblog init-admin`.
10. Reverse proxy: copy `deploy/Caddyfile` or `deploy/nginx.conf`, replace the domain; run `task proxy:lint` beforehand.
11. DNS: point the A/AAAA records at the host.
12. TLS: Caddy obtains certificates automatically; for nginx set the certbot certificate paths.
13. Start: `docker compose up --detach && docker compose ps` shows `(healthy)`.
14. Sign in at the origin in a browser.
15. Create, edit, save and publish a post; confirm the file exists on disk under `site/`.
16. Repository: commit, Check for updates, Push, and confirm the commit on the remote (if configured).
17. Persistence: `docker compose restart`, then replace the container; sessions and content must survive.
18. Backup and restore drill as described in [backup.md](backup.md).
19. Proxy smoke test: `BURZBLOG_PROXY_URL=https://<origin> BURZBLOG_SMOKE_PASSWORD=... uv run --frozen python scripts/proxy-smoke.py`.
20. Deploy spec against a disposable site: `cd web && pnpm exec playwright test -c playwright.deploy.config.ts` with the variables in [containers.md](containers.md#5-validate-the-deployment).

Record the results in issue #5. Only after this does `docs/release.md` continue with the tag, image publication and GitHub release.
