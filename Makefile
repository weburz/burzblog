.PHONY: setup dev check format
setup:
	uv sync --frozen
dev:
	uv run --frozen burzblog --help
check:
	uv run --frozen ruff format --check .
	uv run --frozen ruff check .
	uv run --frozen mypy
	uv run --frozen pytest
	uv build --no-sources --no-build-isolation
format:
	uv run --frozen ruff format .
