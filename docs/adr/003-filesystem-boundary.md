# ADR-003: POSIX descriptor-relative storage

Accepted. Resolve each component with directory descriptors and `O_NOFOLLOW`,
not just string normalization. Refuse symlinks, hard-linked content, non-regular
files and foreign-owned files. Require read revisions for every destructive
source change. Serialize application writers with root-directory flock.

Atomic file publication prevents truncated articles; create and relocation must
not overwrite destinations. Delete means move to a pre-created `.archive`
directory using the same no-clobber rules. Hard delete is not provided.

Costs: POSIX-only initial runtime, private root, existing directories, no ACL/
xattr preservation guarantee, no protection against a hostile same-user process.
External writers do not honor flock automatically; see the explicit race limits
in the architecture and security policy. Never market these checks as a sandbox.
