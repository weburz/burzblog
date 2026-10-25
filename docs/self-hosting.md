# Running BurzBlog

BurzBlog is one binary and one SQLite file. These steps run it under systemd on
a Raspberry Pi or any Linux host, behind HTTPS.

## 1. Get the binary

Build it, or cross-compile for the Pi from another machine:

```sh
task build                 # bin/burzblog for this machine
task release               # dist/ for linux/arm64, linux/arm (Pi 2/3 32-bit), amd64, macOS, Windows
```

Copy the right file to the host as `/usr/local/bin/burzblog`. A 64-bit Raspberry
Pi OS uses `linux-arm64`.

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
sudo -u burzblog burzblog user add ada --data /var/lib/burzblog
journalctl -u burzblog -f
```

Accounts are managed on the host only: `burzblog user add`, `user passwd` and
`user list`, always with `--data /var/lib/burzblog` and as the `burzblog` user.
`user passwd` also signs that account out of every browser.

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

Prefer the environment for the deploy hook so it does not show up in process
listings.

## Backups

Everything lives in `burzblog.db` in the data directory: posts, every earlier
version of every post, accounts and token digests. Back up a consistent copy
while the server runs:

```sh
sudo -u burzblog sqlite3 /var/lib/burzblog/burzblog.db ".backup '/var/lib/burzblog/backup.db'"
```

Or stop the service and copy `burzblog.db` (the `-wal` and `-shm` files are
folded in on a clean stop). To restore, stop the service, put the file back as
`burzblog.db` owned by `burzblog` with mode `0600`, and start it.

For a plain-text copy of every post, drafts and archived ones included, use
**Settings → Backup** in the panel or `burzblog backup [dir]`. It writes
`<status>/<slug>.md` files and a `README.txt`; restore each with
`burzblog push`. It holds current posts only: revisions, accounts and tokens
live only in the database, so the database file remains the complete backup.

For a plain-text copy of your published writing, `burzblog export --out dir`
writes every published post as a Markdown file.

## Upgrades

Replace the binary and restart:

```sh
sudo install -m 0755 burzblog /usr/local/bin/burzblog
sudo systemctl restart burzblog
```

The database schema upgrades itself on start. An older binary refuses to open
a database written by a newer one, rather than damaging it. Take a backup
before upgrading.
