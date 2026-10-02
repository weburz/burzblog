# Dependency decisions

No previous implementation code is reused. Tooling references were inspected
only for conventions (uv, Ruff, typing, pytest and Conventional Commits).

Runtime: `ruamel.yaml` 0.18.16 (MIT). It provides YAML 1.2 round-trip parsing,
preserving comments, key order and quotes better than PyYAML's ordinary dump.
It has a long-lived upstream and a deliberately pinned pre-1.0 API. Its size and
parser surface are justified by frontmatter compatibility; a custom parser is
not. The pure-Python round-trip loader is used. Its optional platform C helper
may be present in the resolved environment, but is not used by this parser.

Sources: [upstream overview](https://yaml.dev/doc/ruamel.yaml/overview/),
[API stability guidance](https://yaml.dev/doc/ruamel.yaml/api/),
[package metadata](https://pypi.org/project/ruamel.yaml/0.18.16/).
These explain capability and provenance, not a claim of absence of vulnerabilities.
No independent security audit has been performed. Review advisories and lockfile
changes before release; resource limits and restricted YAML reduce exposure.

Development/build: Ruff (MIT), mypy (MIT), Pyright (MIT), pre-commit (MIT),
pre-commit-hooks (MIT), pytest (MIT), pytest-cov (MIT), Hatchling (MIT),
and their locked transitive dependencies. Pyright uses Node only for development.
Task orchestrates commands; Crisp checks commit messages; Hadolint checks Dockerfiles.
No frontend bundle
or runtime web framework is installed. Redistribution must retain dependency
license notices; the maintainer's eventual project license must be checked for
compatibility. The lockfile records resolved versions and artifact hashes.
