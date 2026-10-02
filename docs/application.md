# Application API and local authentication

The API powers the [browser interface](browser.md). It manages one
operator-configured content root. Routes call content/Git services, never shell
commands assembled from requests. Read operations consult disk; pagination limits
parsed summaries to 200 per request. Discovery still traverses the content tree.

## Local setup

Use existing content, or create `posts/` and `.archive/` under a new root. Keep
operational state outside both the content directory and the repository. State
must be owned by the service user with mode 0700; its parent must already exist.

```sh
export BURZBLOG_CONTENT_ROOT=/absolute/path/to/content
export BURZBLOG_STATE_DIR=/absolute/private/path/to/burzblog-state
export BURZBLOG_ORIGIN=http://localhost:3000
uv run --frozen burzblog-server init-admin
uv run --frozen burzblog-server serve
```

Initialization asks for a password twice, without command-line arguments or
environment variables. Use at least 12 characters. Re-initialization refuses to
replace credentials. There is no password-reset workflow yet. The server refuses
startup without an initialized administrator. It binds `127.0.0.1:8000` by
default; `--port` changes the port. `--host 0.0.0.0` is only for containers whose
published port is bound to host loopback (see [containers](containers.md)). Do not
expose an unauthenticated CLI wrapper.

With `BURZBLOG_WEB_ROOT` pointing at the generated interface (`web/.output/public`
after `pnpm build`), the same process also serves the browser shell at `/` and
its fingerprinted assets at `/_nuxt/`, with a hash-based Content-Security-Policy.
Neither contains content or credentials. `GET /api/session` is the readiness probe:
it needs no login and returns no content.

For deployment, set the exact HTTPS browser origin and put a same-origin reverse
proxy in front of the loopback service, forwarding everything to it. The proxy
must bound request duration/connections and preserve Host/Origin. Proxy headers
are not trusted. Never use a development HTTP origin for an Internet deployment.
No CORS is enabled. API responses are private/no-store.

Optional `BURZBLOG_REMOTE_URL` and `BURZBLOG_REMOTE_NAME` configure the explicit
operator-approved Git endpoint. Browser requests cannot change it. A private HTTPS
repository additionally uses `BURZBLOG_REMOTE_USERNAME` plus
`BURZBLOG_REMOTE_TOKEN_FILE` (a private 0600 file outside content, Git and state)
and, for privately signed certificates, `BURZBLOG_REMOTE_CA_FILE`. See
[private HTTPS remotes](git.md#private-https-remotes); SSH remains unsupported.
Never embed credentials in a URL or environment variable. No network requests
occur except explicit Git sync.

Startup validates the whole configuration and prints a specific, secret-free
reason when something is wrong (for example a group-readable token file or a
state directory inside the repository).

## API contract

All request/response bodies use JSON. Every mutation sends `Origin` and
`Content-Type: application/json`. Login returns `csrf`; subsequent mutations send
it as `X-CSRF-Token` with the HttpOnly session cookie. Session information exposes
the token only to same-origin clients. Tokens are never persisted in browser
local storage by this API.

- `POST /api/login`: `{password}`; `GET /api/session`; `POST /api/logout`.
- `GET /api/posts?offset=0&limit=100`: summaries and total; malformed files are
  listed as unavailable rather than silently removed.
- `GET /api/post?path=posts/example.md`: path, revision, complete Markdown,
  title and status.
- `POST /api/posts`: title, slug, optional body; creates a draft in `posts/`.
- `PUT /api/post`: path, revision, markdown; never silently retries a stale save.
- `POST /api/publish`: path, revision; changes status metadata, no Git push.
- `POST /api/rename`: path, revision, destination. It moves the filename without
  rewriting slug metadata; edit frontmatter separately when desired.
- `POST /api/archive`: path, revision; reversible archive path returned.
- `POST /api/preview`: markdown; sanitized HTML returned, images omitted.
- `GET /api/git`: safe repository state without absolute root/remote URLs;
  `remote` is `null` or `{name, transport: "https"|"file", authenticated}`.
- `POST /api/git`: action `create_branch`, `switch`, `commit`, `fetch`, `update`
  or `push`; branch, paths and message as relevant. Explicit content-relative
  paths only for commit. Existing Git safety restrictions apply.

Errors have `{error: {code, message}}`. Status 409 means conflict/recovery or
filesystem attention: preserve unsent input, do not automatically resubmit.
Status 502 (`remote`) means the configured Git remote was unreachable, untrusted
or refused the credentials; no local content changed and retrying cannot help
until the operator fixes it. 401 requires login/request verification; 429 is login throttling; 413/422
reject oversized/invalid requests. Validation responses never echo input values.
Markdown is limited to 2 MiB; the entire JSON request to 3 MiB.

## Operational limits

One administrator; fixed 12-hour sessions; at most 20 retained sessions; ten login
attempts per ten minutes globally, including successful attempts. This simple
window is complemented by one concurrent Argon2 verification per application
instance, preserving hashing parameters while bounding parallel memory use. The
attempt window persists across restarts but permits temporary login denial by an attacker.
State is trusted operator material, not safe against another process running as
the same OS user. Back up private state separately with restrictive permissions;
never place it in Git. No database content migration or export is necessary.

Tests cover the API using an in-process HTTP client and local bare Git fixtures.
They are not browser E2E tests or proof of secure Internet deployment.
