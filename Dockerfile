# BurzBlog browser application: one non-root process serves the generated
# browser shell and the authenticated API. Content, authentication state and
# Git secrets are always bind-mounted from the host; nothing persistent lives in
# the image or in anonymous volumes.
FROM ghcr.io/astral-sh/uv:0.12.21@sha256:a7aed3216253ee804de3e2d8afa5073baa1a177335345d43845cd4165e43b711 AS uv

# Static browser build. Node.js and pnpm exist only in this stage.
FROM node:24.14.1-bookworm-slim@sha256:b506e7321f176aae77317f99d67a24b272c1f09f1d10f1761f2773447d8da26c AS web
WORKDIR /web
RUN npm install --global --no-fund --no-audit pnpm@11.15.1
COPY web/ ./
RUN pnpm install --frozen-lockfile && pnpm build

FROM python:3.14.8-slim-bookworm@sha256:c8137f4c460908c8763f281c8f22c431eb5c538514ba9553fc3a89c06b7cfb88 AS builder
COPY --from=uv /uv /usr/local/bin/uv
WORKDIR /app
ENV UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never UV_COMPILE_BYTECODE=1
COPY pyproject.toml uv.lock README.md ./
RUN uv sync --frozen --no-dev --group build --no-install-project
COPY src ./src
RUN uv build --wheel --no-sources --no-build-isolation --out-dir /dist \
    && uv sync --frozen --no-dev --no-install-project \
    && uv pip install --no-deps --compile-bytecode /dist/*.whl

FROM python:3.14.8-slim-bookworm@sha256:c8137f4c460908c8763f281c8f22c431eb5c538514ba9553fc3a89c06b7cfb88 AS runtime
LABEL org.opencontainers.image.source="https://github.com/weburz/burzblog" \
      org.opencontainers.image.description="BurzBlog file-first Markdown writing application"
# Git performs explicit fetch/push; CA certificates verify HTTPS remotes; tini
# reaps Git helper processes. Pinned versions fail closed when Debian supersedes
# them: bump deliberately after reviewing the security update.
RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        git=1:2.39.5-0+deb12u3 \
        ca-certificates=20250419~deb12u1 \
        tini=0.19.0-1+b3 \
    && rm -rf /var/lib/apt/lists/* \
    && python -m pip uninstall --yes --quiet pip
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH=/app/.venv/bin:$PATH \
    BURZBLOG_WEB_ROOT=/app/web
WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
COPY --from=web /web/.output/public /app/web
# Compose overrides this with the host content owner's numeric UID:GID.
USER 1000:1000
EXPOSE 8080
# /api/session answers without authentication and without content.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/api/session', timeout=4)"]
ENTRYPOINT ["tini", "--", "burzblog-server"]
CMD ["serve", "--host", "0.0.0.0", "--port", "8080"]
