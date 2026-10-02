# Running the foundation locally

The authenticated API has a separate [setup guide](application.md); a browser
interface has its own [running guide](browser.md). Container packaging still targets the CLI.

For the local library/CLI, install the development
prerequisites from README, then run `task setup` and `task dev`. Point `--root` at an
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
database for CLI-only use. API installations must separately back up the private
SQLite authentication directory. Keep configuration/secrets outside
content and back them up separately; a private Git token file
([setup](git.md#private-https-remotes)) can instead be re-issued at the Git host
after a restore. Run upgrades explicitly; startup never
migrates Markdown. Review release notes before replacing the installed version.

No server memory or request latency claims apply yet. Performance measurements
must describe the command, dataset, machine and observed results. Native Windows,
network filesystem operation and crash/power-loss fault injection are unverified.
