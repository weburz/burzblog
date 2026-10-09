# ADR-005: Capture before no-clobber publication

Accepted; supersedes ADR-003's replace and link/unlink mutation protocol.

A revision check followed by replacement cannot preserve an external edit made
between those operations. Advisory locks do not bind external editors. A copied
backup made before replacement has the same race.

Each changing save/move creates a mode-0700 journal under `.burzblog-recovery`.
Persist `intent.json` (relative source/destination), `observed.md` (initial bytes)
and `requested.md` (requested bytes). Atomically rename the current source entry
to `original.md` inside that fresh journal. This retains the **actual inode at
capture**, not the previously observed inode. Sync the journal and source parent.
Validate the captured inode/revision. Publish a separate staged file using a
no-clobber hard link; remove its staging link after syncing the destination.
Write a completion marker. Never unlink a captured original, including on success.

An external replacement before capture is retained; an external creation after
capture wins and causes publication to fail. Late writes through an open external
descriptor land on retained `original.md`. No application step unlinks the only
name of a captured inode. This guarantees recoverability, not automatic merging
or visibility of every late write in the normal article path.

Tradeoffs: save/move is now a multi-step recoverable operation, with a temporary
gap in the source path. Crashes can leave it absent. Reads may briefly report
missing content and should be retried; mutation failures require journal review.
Cross-filesystem moves fail before capture. Journals are never auto-pruned because
an external descriptor can remain open indefinitely. Disk growth is deliberate;
operators stop writers, back up and manually remove reviewed journals. Underlying
filesystem/power-loss guarantees still depend on fsync support and the OS.

Recovery files are ordinary files, not a canonical database. Hidden journals are
excluded from post discovery and must be excluded from application Git commits.
No directory tree, mount or same-user adversary sandbox is claimed.

## Amendment (2026-10-09, #42)

Before capturing, both the source and destination folders must be writable by
the service user; otherwise the operation is refused with nothing moved. If
publication still fails after capture and before the destination link exists,
the captured inode is linked back to its source name with a no-clobber hard link
and only then is its `original.md` name removed, so the inode always keeps a
name. The journal keeps `observed.md`, `requested.md` and a `restored` marker,
and the request fails as an ordinary conflict. When the source name is taken
(an external creator, or a save whose destination is the source), nothing is
linked back and the journal is left as described above.
