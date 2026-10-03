# Backup and restore

BurzBlog keeps everything in ordinary host directories, so backups are ordinary
archives. `task docker:check` (`scripts/docker-smoke.py`) rehearses the same
steps on disposable data: stop → archive with preserved permissions → destroy →
restore without the secret (refused) → restore the secret → verify.

| Directory | Contains | Backup handling |
| --- | --- | --- |
| `site/` | Markdown, `.archive`, `.burzblog-recovery`, `.git` | normal backup |
| `state/` | login database (Argon2id hash, sessions) | normal backup, keep private |
| `compose.yaml`, `.env` | deployment configuration (no secrets) | normal backup |
| `secrets/` | Git token, private CA | **separate, encrypted, or not at all** |

## Back up

Run as the deployment user (never `sudo`), from the deployment directory:

```sh
cd ~/burzblog
docker compose stop                 # no writes during the copy
stamp=$(date +%Y%m%d-%H%M%S)
mkdir -p -m 0700 ~/backups
tar --numeric-owner --create --preserve-permissions --gzip \
  --file ~/backups/burzblog-$stamp.tar.gz site state compose.yaml .env
docker compose start
```

Stopping first makes the SQLite file and Git repository consistent; the stop
waits up to 45 seconds for an in-flight Git operation. Downtime is the copy time.
Copy the archive off the host (another disk, `rsync`, `restic`, your provider's
snapshots). The archive contains the password hash and session tokens: keep it
private (mode 0600, encrypted at rest off-host).

**Git secrets.** Prefer not backing up `secrets/` at all: after a restore, issue a
new token at the Git host (and revoke the old one). If you must keep a copy,
archive it separately and encrypt it, never alongside content backups:

```sh
tar --create --gzip --directory ~/burzblog secrets | gpg --symmetric \
  --output ~/backups/burzblog-secrets-$stamp.tar.gz.gpg
```

## Restore

On the same or a new host with Docker and the same image reference:

```sh
mkdir -p ~/burzblog && cd ~/burzblog
tar --extract --preserve-permissions --gzip --file /path/to/burzblog-<stamp>.tar.gz
install -d -m 0700 secrets
# Either write a NEW token (preferred) …
(umask 077; IFS= read -rs token && printf '%s\n' "$token" > secrets/git-token)
# … or restore the separate secret archive:
#   gpg --decrypt burzblog-secrets-<stamp>.tar.gz.gpg | tar --extract --gzip
docker compose up --detach
docker compose ps                   # (healthy)
```

Restore as the same numeric UID/GID recorded in `.env` (`id -u`/`id -g`); on a new
host create the user with those IDs, or update `.env` and have an administrator
`sudo chown -R` the restored `site` and `state` to the new user before starting. Directory modes are kept by
`--preserve-permissions` (`state` and `secrets` must be 0700).

If the remote is configured but the token/CA file is missing, the service
refuses to start and `docker compose logs` says which file is missing — restore
the secret (or temporarily remove the `BURZBLOG_REMOTE_*` lines) and start again.

## Verify after restore

1. Sign in with the existing password (sessions from before the backup also work).
2. Open a few posts; `git -C site log --oneline -3` and `git -C site remote -v`
   match what you expect; optionally `git -C site fsck`.
3. Edit and save one post; then use the Repository view: Check for updates, Push.
4. If anything reports recovery, follow [recovery](recovery.md) before retrying.

Revisions are content hashes, so an editor tab opened before the backup can still
save if the file is unchanged, and is refused (conflict) if it changed — the
drill checks both.
