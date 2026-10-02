# ADR-007: Explicit commits and recoverable scoped Git transitions

Accepted. Native checkout/pull can unlink a concurrently replaced working file.
Preserving only an earlier copy would repeat issue #2. Do not ask Git to update
working content. Compute and validate a bounded tree delta, then apply its
Markdown changes through the recovery-aware store. Update the index with
`read-tree` (no `-u`), and finally switch HEAD or compare-and-swap the branch ref.

Before changing content, persist `.git/burzblog-transition.json`. A failed
transition keeps that marker and captured content, and blocks subsequent Git
mutations until externally reviewed. This is a recoverable multi-step operation,
not a Git/filesystem transaction. Application Git writers serialize using flock
on the metadata directory; external Git metadata mutations must be coordinated
by the operator. External content editors do not need to honor that lock.

Require a clean working state for switch/update. Only regular non-executable
Markdown within the configured content root may differ across the trees. Reject
other paths/modes, missing target directories, invalid Markdown, and transitions
over 200 files/16 MiB before changing content. Removed files are archived. Content
must match prior Git blob bytes; conversion/filter-dependent trees require
external tooling. A concurrent source change aborts or remains in recovery.

Commits explicitly stage selected validated content paths and use `commit --only`
so unrelated staged files are excluded and preserved in the index. Hooks and
signing commands are disabled. No automatic identity is invented.

Fetch/push use an explicit operator-provided endpoint, never a repository URL
implicitly. Only credential-free HTTPS and vetted local bare fixtures are
supported now. Fetch has an explicit branch-to-remote-tracking refspec. Push has
an explicit branch refspec, never force. No implicit fetch, merge or rebase.
Update requires ancestry for a fast-forward. Local receiving hooks execute in
the same OS account, so local remotes with active hooks or custom config are
refused. HTTPS server-side hooks belong to that remote server's operator.

Private remote credentials/SSH and browser remote configuration are deferred;
do not expose arbitrary endpoints to untrusted HTTP clients. (Superseded in part:
ADR-010 adds operator-configured private HTTPS credentials; SSH and browser
remote configuration remain out of scope.)
