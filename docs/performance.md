# Local measurement baseline

Run `task measure` after `task web:check`. It starts the real production process
(Python API serving the generated browser shell) on loopback port 3000, prepares
1,000 ordinary Markdown files in a temporary Git repository, and measures requests
through that same-origin server. It refuses to run when port 3000 is occupied.
It never uses configured operator content or credentials. The temporary fixture
is deleted after the process stops. No network remote is contacted.

The script reports startup readiness, Linux process RSS, 20 timed requests after
one warmup for each operation, and actual emitted asset bytes. Readiness timing
begins immediately after spawning the process; fixture generation and password
initialization are excluded. RSS is sampled after readiness, before authenticated
work; it is not peak memory. Login permits only one concurrent Argon2 verification
per application instance without reducing hashing strength. Do not multiply
workers without accounting for independent per-process memory limits.

## Observed on 2026-10-02 (single-process topology)

Local Linux x86_64 workstation (WSL2), Python 3.14.8, one process serving the
generated shell and the API. These are a sanity-check baseline, not a VPS
capacity promise, a cold-disk benchmark, or an Internet latency measurement.

| Measurement | Observed |
| --- | ---: |
| Readiness (shell served) | 322.41 ms |
| Idle RSS | 58.73 MiB |
| List first 100 of 1,000 files, median / p95 | 168.81 / 186.82 ms |
| Recoverable save, median / p95 | 14.46 / 16.87 ms |
| Git status, median / p95 | 23.56 / 28.51 ms |
| All emitted JS, raw / per-file gzip sum | 380,536 / 127,830 bytes |
| All emitted CSS, raw / per-file gzip sum | 210,865 / 34,811 bytes |
| All font subsets, woff2 | 184,264 bytes |
| Complete `.output` regular-file bytes | 780,355 bytes |

The earlier two-process topology (API plus a Nuxt Node server proxying `/api/`)
measured 58.62 MiB API + 76.43 MiB frontend idle RSS and 172.03/16.22/23.33 ms
list/save/status medians; removing the Node runtime saves its memory and a proxy
hop without changing request costs materially. The container image measured
238.6 MB by `docker image inspect` (see [containers](containers.md)).

All emitted assets include deferred/error chunks and font subsets; they are not
the initial page transfer. Browser caching and actual network transfer are not
measured. Build tool `node_modules` is much larger and is not the deployment
artifact. Measurements vary with filesystem, CPU, caches and concurrent work.
An earlier run measured list/save/status medians of 200.99/16.70/24.83 ms; report
that variation rather than treating the table as a fixed performance guarantee.

List discovery remains O(number of directory entries). Summaries of readable
files are kept in process memory and reused while a file's inode, size, mtime and
ctime are unchanged; any write, replacement, rename or permission change on disk
invalidates that file's entry, and opening a post for editing always reads disk.
A status or text filter needs every file's summary, so the first filtered request
after start-up reads every file. There is no persistent index, worker or watcher.
Measure a real large repository before deciding whether a rebuildable index is
justified.

Observed on 2026-10-09 with the summary cache (same 1,000-file fixture, Python
3.14.8, on a workstation also running other test servers, so absolute numbers are
higher than above): first filtered request 2,790 ms; afterwards the first page of
100 by path 11.44 / 15.28 ms, newest first 9.89 / 13.37 ms and a status plus text
filter over all files 14.59 / 31.17 ms (median / p95). Without the cache the same
filter measured 2,321 / 3,129 ms on every request. Recovery journals intentionally accumulate; retain backups and follow
the manual retention guidance rather than deleting late-writer recovery copies.
