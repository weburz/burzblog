# ADR-006: Bounded Git subprocesses and conservative repository configuration

Accepted. A separate Git domain uses the installed Git executable with argument
arrays, isolated environment, no shell, disabled hooks/fsmonitor/global/system
configuration, no optional index locks, and bounded stdout/time. Git provides
normal repository semantics without a provider API or additional runtime library.
NUL-delimited porcelain preserves unusual paths; filenames are never interpreted
as command arguments during read-only inspection. Errors never expose stderr.

Before status, inspect the local config with `--no-includes` and reject settings
outside a small documented allowlist. This prevents clean filters, external diff,
credential helpers, includes and shell commands from being invoked by ostensibly
read-only operations. Submodules are ignored, never recursively inspected.
Remote names are exposed; URLs are withheld to prevent embedded-secret leakage.

Initial support: normal non-bare SHA-1 working trees, including unborn and detached
HEAD, with content roots inside the repository. Linked worktrees, shallow clones,
alternate object stores and additional config files are refused explicitly.
Ahead/behind reflects local refs only; no implicit network fetch occurs.

Trust boundary: operator owns the root and `.git`; concurrent **content** edits
are expected, but hostile concurrent mutation of Git metadata/configuration is
outside this boundary. Git is native code and must be patched by the operator.
This is not an OS sandbox for malicious Git binaries or repository metadata races.
Future Git writes must use explicit content selection and preserve recovery data;
checkout/update must not reintroduce the external-writer loss fixed by ADR-005.

References: [Git porcelain](https://git-scm.com/docs/git-status),
[Git configuration](https://git-scm.com/docs/git-config).
