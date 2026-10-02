FROM ghcr.io/astral-sh/uv:0.12.21@sha256:a7aed3216253ee804de3e2d8afa5073baa1a177335345d43845cd4165e43b711 AS uv
FROM python:3.14.8-slim-bookworm@sha256:c8137f4c460908c8763f281c8f22c431eb5c538514ba9553fc3a89c06b7cfb88 AS builder

COPY --from=uv /uv /usr/local/bin/uv
WORKDIR /app
ENV UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
COPY pyproject.toml uv.lock README.md ./
RUN uv sync --frozen --no-dev --group build --no-install-project
COPY src ./src
RUN uv build --wheel --no-sources --no-build-isolation --out-dir /dist \
    && uv sync --frozen --no-dev --no-install-project \
    && uv pip install --no-deps /dist/*.whl

FROM python:3.14.8-slim-bookworm@sha256:c8137f4c460908c8763f281c8f22c431eb5c538514ba9553fc3a89c06b7cfb88 AS runtime
LABEL org.opencontainers.image.source="https://github.com/weburz/burzblog" \
      org.opencontainers.image.description="BurzBlog Markdown foundation CLI"
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
RUN mkdir /content && chown 1000:1000 /content
USER 1000:1000
ENTRYPOINT ["/app/.venv/bin/burzblog", "--root", "/content"]
CMD ["list"]
