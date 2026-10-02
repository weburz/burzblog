# Recovering an interrupted save or move

Every changing save, rename or archive keeps a journal in
`CONTENT_ROOT/.burzblog-recovery/<unique-id>/`. No-op saves create none.

- `intent.json`: original relative path and intended destination.
- `observed.md`: bytes read before capture.
- `requested.md`: bytes BurzBlog intended to publish.
- `original.md`: actual captured inode; may include late external-editor writes.
- `complete`: publication completed, but the original is retained regardless.
- `publish.md`: temporary publication inode if interrupted; may be hard-linked
  to the destination. It is not another canonical article.

On a recovery error: stop writers, back up the entire content directory and
inspect the journal alongside any current source/destination. Open ordinary
Markdown files in an editor, compare them, and copy the desired content to a
**new, unused filename** first. Never blindly overwrite the current destination.
If a surviving `publish.md` shares an inode with a destination, remove only that
staging link after backup/review to restore the store's single-link requirement.

Do not follow a symlink found in a journal: an externally replaced symlink can
be captured, but the store refuses to read it. A missing `original.md` means
capture did not complete; check the normal source. A missing completion marker
does not prove publication failed: inspect both locations before retrying.

Journals are private (0700 directories, 0600 metadata/request snapshots); the
captured original retains its mode inside the private directory. They are never
automatically deleted, even after successful saves, because another editor may
still hold an open descriptor to `original.md`. Stop external writers before
manual retention cleanup. Back them up; do not commit them or serve them over HTTP.
Recovery is manual in this milestone. Disk exhaustion aborts before capture
where staging fails; errors after capture retain original/requested files.
