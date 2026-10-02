# Local measurement baseline

Run `task measure` after `task web:check`. It starts the real Python API and built
Nuxt server on loopback, prepares 1,000 ordinary Markdown files in a temporary
Git repository, and measures requests through the frontend proxy. It refuses to
run when ports 8000 or 3000 are occupied. It never uses configured operator
content or credentials. The temporary fixture is deleted after child processes
stop. No network remote is contacted.

The script reports startup readiness, Linux process RSS, 20 timed requests after
one warmup for each operation, and actual emitted asset bytes. Readiness timing
begins immediately after spawning each process; fixture generation and password
initialization are excluded. RSS is sampled after readiness, before authenticated
work; it is not peak memory. Login permits only one concurrent Argon2 verification
per application instance without reducing hashing strength. Do not multiply
workers without accounting for independent per-process memory limits.

## Observed on 2026-10-02

Local Linux x86_64 workstation, Python 3.14.8, Node 24.14.1, one API process and
one Nuxt production process. These are a sanity-check baseline, not a VPS capacity
promise, a cold-disk benchmark, or an Internet latency measurement.

| Measurement | Observed |
| --- | ---: |
| API readiness | 316.42 ms |
| Frontend readiness | 111.30 ms |
| API idle RSS | 58.62 MiB |
| Frontend idle RSS | 76.43 MiB |
| List first 100 of 1,000 files, median / p95 | 172.03 / 186.17 ms |
| Recoverable save, median / p95 | 16.22 / 18.77 ms |
| Git status, median / p95 | 23.33 / 24.18 ms |
| All emitted JS, raw / per-file gzip sum | 379,960 / 127,558 bytes |
| All emitted CSS, raw / per-file gzip sum | 210,865 / 34,811 bytes |
| All font subsets, woff2 | 184,264 bytes |
| Complete `.output` regular-file bytes | 2,675,006 bytes |

All emitted assets include deferred/error chunks and font subsets; they are not
the initial page transfer. Browser caching and actual network transfer are not
measured. Build tool `node_modules` is much larger and is not the deployment
artifact. Measurements vary with filesystem, CPU, caches and concurrent work.
An earlier run measured list/save/status medians of 200.99/16.70/24.83 ms; report
that variation rather than treating the table as a fixed performance guarantee.

List discovery remains O(number of directory entries), with only the requested
page parsed into summaries. There is no persistent index, worker or watcher.
Measure a real large repository before deciding whether a rebuildable index is
justified. Recovery journals intentionally accumulate; retain backups and follow
the manual retention guidance rather than deleting late-writer recovery copies.
