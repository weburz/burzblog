# Running the foundation locally

This phase is a local library/CLI, not a network CMS. Install Python 3.12+ and uv,
clone the repository, then run `make setup` and `make dev`. Point `--root` at an
existing directory owned by the current effective user. Use a dedicated account;
root and subdirectories must not be group/world writable. Content files must
also have the account's effective group. Symlinked ancestors are rejected.

Create collection folders and `.archive` yourself. A typical layout:

```text
content/
  posts/
  pages/
  .archive/
```

Back up content, archive and any `.git` directory with normal filesystem tools
while writers are stopped. Restore with those same tools. There is no operational
database or application secret yet. Keep future configuration/secrets outside
content and back them up separately. Run upgrades explicitly; startup never
migrates Markdown. Review release notes before replacing the installed version.

No server memory or request latency claims apply yet. Performance measurements
must describe the command, dataset, machine and observed results. Native Windows,
network filesystem operation and crash/power-loss fault injection are unverified.
