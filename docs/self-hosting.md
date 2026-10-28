# Running BurzBlog

BurzBlog is one binary and one SQLite file. These steps run it under systemd on
a Raspberry Pi or any Linux host, behind HTTPS.

## 1. Get the binary

Build it, or cross-compile for the Pi from another machine:

```sh
task build                 # bin/burzblog for this machine
task release               # dist/ for linux/arm64, linux/arm (Pi 2/3 32-bit), amd64, macOS, Windows
```

Copy the right file to the host and name it `burzblog`. A 64-bit Raspberry Pi OS
uses `linux-arm64`.

## 2. Install the service

```sh
sudo useradd --system --home-dir /var/lib/burzblog --shell /usr/sbin/nologin burzblog
sudo install -m 0755 burzblog /usr/local/bin/burzblog
sudo install -m 0644 deploy/burzblog.service /etc/systemd/system/burzblog.service
sudo systemctl edit burzblog
```

In the override, set the public address people will open:

```ini
[Service]
Environment=BURZBLOG_ORIGIN=https://blog.example.com
```

The origin must match the address in the browser exactly (scheme, host and
port). Browser requests from anywhere else are refused.

To rebuild your website after publishing, put the deploy hook URL in a private
file, because it is a secret:

```sh
sudo install -d -m 0755 /etc/burzblog
echo 'BURZBLOG_DEPLOY_HOOK=https://api.vercel.com/v1/integrations/deploy/...' | sudo tee /etc/burzblog/env >/dev/null
sudo chmod 0600 /etc/burzblog/env
```

Start it and create the first account:

```sh
sudo systemctl enable --now burzblog
journalctl -u burzblog -n 20
```

While the server has no accounts, it logs a one-time setup link on every start:

```text
msg="no accounts yet; open https://blog.example.com/setup#token=... to create the first one (valid for 60 minutes; restart to get a new link), or run: burzblog user add <name> --data /var/lib/burzblog"
```

Open that link in your browser, choose a username and a password of at least 12
characters, and you are signed in. The link works once and for 60 minutes; after
that, or once any account exists, it is dead and `sudo systemctl restart
burzblog` prints a new one if it is still needed. The token sits after the `#`,
so browsers never send it to your reverse proxy, and the panel removes it from
the address bar as soon as the page opens. Anyone who can read the service log
can use the link, so open it before you share the logs.

Prefer the terminal? Create the account on the host instead; that also ends
first-run setup:

```sh
sudo -u burzblog burzblog user add ada --data /var/lib/burzblog
```

Later accounts are managed on the host only: `burzblog user add`, `user passwd`,
`user role` and `user list`, always with `--data /var/lib/burzblog` and as the
`burzblog` user. Passwords need at least 12 characters.
`user passwd` also signs that account out of every browser.

An account is an admin or an author. The account first-run setup creates is an
admin, and so is the first account `user add` makes; later ones are authors
unless you add `--role admin`. An author
writes and publishes posts and manages their own password, two-factor sign-in
and tokens; an admin can also delete posts, manage everyone's tokens, change
settings, deploy, download backups and read the audit log (see
[security](security.md) for the full list). Change an account's role with
`burzblog user role <name> admin|author --data /var/lib/burzblog`; the last
admin cannot be demoted. `burzblog audit --data /var/lib/burzblog` prints the
audit log on the host.

If someone loses their authenticator app and recovery codes,
`burzblog user 2fa-reset <name> --data /var/lib/burzblog` turns two-factor
sign-in off for them and signs them out of every browser. Their terminal logins
keep working; they can set two-factor up again in Settings.

## 3. HTTPS in front

BurzBlog listens on `127.0.0.1:8080` and does not terminate TLS. Put a reverse
proxy in front:

- [deploy/Caddyfile](../deploy/Caddyfile): Caddy obtains certificates itself.
- [deploy/nginx.conf](../deploy/nginx.conf): nginx with your certificates.

Both forward the original `Host` header and limit request bodies to 4 MB.
`task proxy:lint` validates both files with pinned images.

On a home network without a public address, a tunnel (for example Cloudflare
Tunnel) to `127.0.0.1:8080` works the same way; set `BURZBLOG_ORIGIN` to the
tunnel's public HTTPS address. Your website's build must be able to reach this
address to download posts.

## Settings

| Flag | Environment | Default |
|---|---|---|
| `--addr` | `BURZBLOG_ADDR` | `127.0.0.1:8080` |
| `--data` | `BURZBLOG_DATA`, then systemd's `STATE_DIRECTORY` | `./burzblog-data` |
| `--origin` | `BURZBLOG_ORIGIN` | `http://<addr>` |
| `--deploy-hook` | `BURZBLOG_DEPLOY_HOOK` | none |
| `--check-updates` | `BURZBLOG_CHECK_UPDATES` (`true` or `false`) | off |
| `--site-url` | `BURZBLOG_SITE_URL` | none (feeds and sitemap answer 409) |
| `--site-title` | `BURZBLOG_SITE_TITLE` | the site URL's host |
| `--site-description` | `BURZBLOG_SITE_DESCRIPTION` | the title |
| `--site-language` | `BURZBLOG_SITE_LANGUAGE` | `en` |
| `--post-path` | `BURZBLOG_POST_PATH` | `/blog/{slug}` |
| `--tag-path` | `BURZBLOG_TAG_PATH` | none (no tag pages in the sitemap) |

Prefer the environment for the deploy hook so it does not show up in process
listings.

### Update checks

BurzBlog contacts nobody unless you ask. With `--check-updates` (or
`BURZBLOG_CHECK_UPDATES=true`) it looks for a newer release about 30 seconds
after it starts and then every 24 hours. Admins see the result in Settings and
as a dot on the Settings tab; authors do not, and the content API never
includes it.

Each check is one `GET` to
`https://api.github.com/repos/weburz/burzblog/releases/latest`. The only thing
it carries is a `User-Agent` such as `burzblog/1.2.0`, which holds your BurzBlog
version. Nothing about your posts, accounts or the instance's address is sent,
though GitHub sees your server's IP address as it would for any request. The
check has a 10 second timeout, reads at most 1 MiB, is not retried before the
next day and keeps its answer in memory only. If it fails, BurzBlog logs one
warning and carries on. A development build (a version that is not
`MAJOR.MINOR.PATCH`) never reports an update. Remove the flag and restart to
stop the checks.

When a newer version exists, upgrade with `sudo burzblog upgrade` and restart
the service on a systemd host, or pull the new image and recreate the
container.

The `--site-*`, `--post-path` and `--tag-path` settings describe your public
website (not BurzBlog's own address, `--origin`). They only build the links in
the feeds and sitemap. `--site-url` is an address without a path, such as
`https://example.com`; the paths must start with `/` and hold `{slug}` or
`{tag}` once. They are not secret. BurzBlog refuses to start when one is
invalid. See [publishing](publishing.md#feeds-and-sitemap).

## Backups

Everything a server holds is in its data directory: `burzblog.db` (posts,
every earlier version of every post, accounts with their password hashes,
two-factor secrets and roles, token digests, tag labels and the audit log) and
the `media` folder with the uploads. A disk that fails takes all of it with it,
so keep a copy on another machine or in storage elsewhere.

### Full snapshots

`burzblog snapshot create` writes everything to one file while BurzBlog keeps
running: the database is copied consistently (SQLite `VACUUM INTO`), then every
upload it records is added, with a manifest of sizes and SHA-256 checksums.
It never upgrades the database, so a newly installed binary can snapshot a
server that has not restarted onto it yet. Run it as the service user:

```sh
sudo -u burzblog burzblog snapshot create --data /var/lib/burzblog
# ✓ Wrote /var/lib/burzblog/snapshots/burzblog-snapshot-20261027T031700.000Z.tar.gz (4.2 MB).
```

Without a target it writes to the `snapshots` folder in the data directory. A
target ending in `.gz` or `.enc` is the file to write; anything else is a
folder, made when missing. `-` writes to standard output. `--keep N` deletes
all but the newest N snapshots in the folder and touches no other file. An
upload the database records but the `media` folder lacks is reported and
skipped, so one lost file does not stop backups. Each snapshot is recorded in
the audit log, and admins see the newest one under **Settings → Backup**, which
warns when there is none or it is more than a week old.

A snapshot is a plain `.tar.gz` you can open with `tar`, holding password
hashes and two-factor secrets. **Encrypt it when it leaves the server**: put a
passphrase of at least 12 characters in `BURZBLOG_SNAPSHOT_PASSPHRASE` or a
file passed with `--passphrase-file` (or type one with `--encrypt`), and the
snapshot is sealed with it and named `.tar.gz.enc` (see
[security](security.md#snapshots)). Encryption also proves on restore that
nobody changed the file; a plain snapshot has checksums against damage, not
against someone who can edit it. Keep the passphrase somewhere other than
the server: without it nobody can restore the snapshot, including you.

Make one every night with a systemd timer, keeping two weeks, encrypted:

```sh
sudo install -d -o burzblog -g burzblog -m 0700 /var/backups/burzblog
sudo install -m 0600 -o burzblog -g burzblog /dev/null /etc/burzblog/snapshot-passphrase
sudo -e /etc/burzblog/snapshot-passphrase     # one line: the passphrase
```

```ini
# /etc/systemd/system/burzblog-snapshot.service
[Unit]
Description=BurzBlog snapshot

[Service]
Type=oneshot
User=burzblog
ExecStart=/usr/local/bin/burzblog snapshot create /var/backups/burzblog --keep 14 \
  --data /var/lib/burzblog --passphrase-file /etc/burzblog/snapshot-passphrase
```

```ini
# /etc/systemd/system/burzblog-snapshot.timer
[Unit]
Description=Nightly BurzBlog snapshot

[Timer]
OnCalendar=*-*-* 03:17:00
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now burzblog-snapshot.timer
sudo systemctl start burzblog-snapshot.service   # one now, to check it works
```

That folder is still on the same disk. Copy it off with the tool you already
use (`rclone`, `restic`, `rsync`), or pull snapshots from another machine
instead, which needs nothing extra on the server:

```sh
ssh blog.example.com 'sudo -u burzblog burzblog snapshot create - --data /var/lib/burzblog' > blog-$(date +%F).tar.gz
```

Check a copy now and then: `burzblog snapshot verify <file>` reads the whole
snapshot, checks every checksum and the database's integrity, and reports what
it holds, without restoring anything. It unpacks into the system's temporary
folder (or `--work <folder>`), which needs room for the whole snapshot.

### Automatic snapshots

The server can make snapshots on its own and send them off the machine, to a
folder or to S3-compatible object storage (AWS S3, Cloudflare R2, Backblaze
B2, Hetzner Object Storage, MinIO and others). Set it in the service override
(`sudo systemctl edit burzblog`), or the environment of the container:

| Setting | |
|---|---|
| `BURZBLOG_SNAPSHOT_EVERY` | How often, such as `24h` or `6h` (at least `1h`). Unset, automatic snapshots are off. |
| `BURZBLOG_SNAPSHOT_TO` | A folder, or `s3://<bucket>/<prefix>`, one prefix per blog. Default `<data>/snapshots`. |
| `BURZBLOG_SNAPSHOT_KEEP` | How many to keep, default `14`; `0` keeps all. |
| `BURZBLOG_SNAPSHOT_PASSPHRASE` or `BURZBLOG_SNAPSHOT_PASSPHRASE_FILE` | The passphrase, or a file holding it (Docker and systemd secrets). Required for object storage. |
| `BURZBLOG_SNAPSHOT_HEARTBEAT` | Optional https address to `GET` after every snapshot that worked, for a monitor such as healthchecks.io that alerts you when the pings stop. |
| `BURZBLOG_S3_ENDPOINT` | The storage's https address: `https://<account>.r2.cloudflarestorage.com`, `https://s3.<region>.amazonaws.com`, `https://s3.<region>.backblazeb2.com`, `https://<region>.your-objectstorage.com`. |
| `BURZBLOG_S3_ACCESS_KEY_ID`, `BURZBLOG_S3_SECRET_ACCESS_KEY` (or `BURZBLOG_S3_SECRET_ACCESS_KEY_FILE`) | A key for the bucket. Give it read, write, list and delete on that bucket only. Without delete, snapshots are stored but old ones stay, and Settings says so. |
| `BURZBLOG_S3_REGION` | The signing region, default `us-east-1` (right for R2 and MinIO); set your bucket's region on AWS and B2. |
| `BURZBLOG_S3_VIRTUAL_HOSTED` | `true` to put the bucket in the host name instead of the path, for services that need it. |

```ini
[Service]
Environment=BURZBLOG_SNAPSHOT_EVERY=24h
Environment=BURZBLOG_SNAPSHOT_TO=s3://example-backups/blog/
# The S3 key and the passphrase, in a file of mode 0600.
EnvironmentFile=/etc/burzblog/snapshot.env
```

The first snapshot is made a minute after the server starts when none is
recent, then one every interval after the newest, whether the schedule or
`burzblog snapshot create` made it. A failure is logged, recorded in the audit
log (`snapshot.fail`), shown to admins under **Settings → Backup**, and tried
again within the hour; the heartbeat is only sent for one that worked.

In object storage each upload is stored once, encrypted, beside the
snapshots (`<prefix>media/`), named by its content, so after the first night
a snapshot sends the database and only the uploads that are new. Keeping N
deletes older snapshots and then, a day later, the uploads that no kept
snapshot needs. Use one prefix per blog: two servers writing to the same
prefix would prune each other's snapshots. On one server a scheduled snapshot
and one made by hand wait for each other.
Storage keys, the passphrase and the heartbeat address are never shown in the
panel or written to the log. `burzblog snapshot create s3://<bucket>/<prefix>`
makes one by hand with the same settings, and `burzblog snapshot list` shows
what is there.

### Restoring

On the new server, install BurzBlog as above, then restore before or instead
of the first start, as the service user:

```sh
sudo systemctl stop burzblog
sudo -u burzblog burzblog snapshot restore blog.tar.gz.enc --data /var/lib/burzblog \
  --passphrase-file /etc/burzblog/snapshot-passphrase
sudo systemctl start burzblog
```

From object storage, give the bucket and prefix for the newest snapshot, or a
full key from `burzblog snapshot list` for another, with the same `BURZBLOG_S3_*`
settings:

```sh
sudo -u burzblog env BURZBLOG_S3_ENDPOINT=... BURZBLOG_S3_ACCESS_KEY_ID=... BURZBLOG_S3_SECRET_ACCESS_KEY=... \
  burzblog snapshot restore s3://example-backups/blog/ --data /var/lib/burzblog \
  --passphrase-file /etc/burzblog/snapshot-passphrase
```

Restore refuses while a server runs with the data directory. The whole
snapshot is checked before anything changes, so a damaged one leaves the
directory as it was. A directory that already holds a blog is refused; with
`--force` its database and uploads are moved to
`<data>/backups/replaced-<UTC time>/`, not deleted. Run as root, it gives the
restored files to the owner of the data directory. Accounts, sessions,
two-factor and tokens come back as they were, uploads keep their addresses,
and a snapshot from an older BurzBlog is upgraded during the restore (with a
backup first). Keep the same public address (`BURZBLOG_ORIGIN`): posts link
to their uploads by full address.

### Other copies

**Settings → Backup → Download all posts** and `burzblog backup [dir]` give a
plain-text copy of every current post, drafts and archived ones included, as
`<status>/<slug>.md` files with a `README.txt` and the uploads in `media/`.
Restore a post with `burzblog push`. They hold current posts only: earlier
versions, accounts and tokens are only in a snapshot. `burzblog export --out
dir` writes every published post as a Markdown file.

Without BurzBlog at hand, a stopped server's data directory is also a complete
copy: `burzblog.db` (the `-wal` and `-shm` files are folded in on a clean stop)
and `media/` (`cp -a` keeps the owner and modes `0600`/`0700`).

Uploads default to 10 MiB each (`--max-upload-mb`, `BURZBLOG_MAX_UPLOAD_MB`);
raise the request-size limit of your reverse proxy to match (the example
configs allow 12 MiB).

## Upgrades

On a binary or systemd install, `burzblog upgrade` replaces the binary with the
latest release:

```sh
burzblog upgrade --check      # exit 0 up to date, 7 update available
sudo burzblog upgrade         # download, verify, replace
sudo systemctl restart burzblog
```

It downloads `burzblog-<version>-<os>-<arch>` and `SHA256SUMS-<version>` from
the GitHub release over https only (at most 200 MiB), refuses a file whose
SHA-256 sum differs from the list, and swaps the binary in with a rename in the
same directory, so a failure leaves the old binary in place. It writes to the
directory of the running binary: if that is not writable it says so and
suggests `sudo`. It never restarts the service and never runs what it
downloaded. `--version vX.Y.Z` picks a release, `--force` allows a downgrade or
a reinstall, and a development build (`dev`) only upgrades with `--version`.
You can also replace the binary by hand (`sudo install -m 0755 burzblog
/usr/local/bin/burzblog`).

Containers are upgraded by pulling the new image
(`docker compose pull && docker compose up -d`); `burzblog upgrade` refuses to
run when it finds `/.dockerenv`, `/run/.containerenv`, or `BURZBLOG_DATA=/data`
with BurzBlog as process 1.

### The database upgrades itself, with a backup first

The schema is upgraded when the new binary starts. If the database is older
than the binary, BurzBlog first writes a consistent copy of it (SQLite
`VACUUM INTO`, safe while the file is in use) to

```text
<data>/backups/burzblog-schema<N>-<UTC time>.db
```

where `<N>` is the schema it had, logs the path, and then migrates. If the copy
cannot be made (disk full, permissions on the data directory) it stops with an
error and does not touch the database. A new database and one already current
get no copy. The newest five of these files are kept and older ones are
deleted; nothing else in `backups/` is touched. The folder is `0700` and the
files `0600`. `burzblog backup` and **Settings → Backup** do not include it;
if you back up the data directory itself, it is included.

An older binary refuses to open a database written by a newer one, rather than
damaging it. To go back to the older version after an upgrade:

1. Stop the service: `sudo systemctl stop burzblog`.
2. Delete `burzblog.db-wal` and `burzblog.db-shm` next to the database.
3. Copy the backup over the database:
   `sudo -u burzblog cp /var/lib/burzblog/backups/burzblog-schema<N>-<time>.db /var/lib/burzblog/burzblog.db`
   (mode `0600`, owned by `burzblog`). Anything written since the upgrade is
   lost.
4. Install the older binary and start the service.

Uploads in `media/` are not touched by migrations, so they need no restoring.
Take your own backup (see above) before an upgrade as well; the automatic copy
sits on the same disk.

`burzblog upgrade` calls the GitHub API and downloads release files without
credentials, so it works only while the releases are publicly downloadable. Its
tests run against local test servers.
