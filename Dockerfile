# BurzBlog in a container: one static binary serving the API and the
# embedded panel. The database lives in /data, a bind mount or a named volume;
# nothing persistent is in the image. A plain binary under systemd is the
# primary deployment; see docs/self-hosting.md.

# The browser panel. Node.js and pnpm exist only in this stage.
FROM --platform=$BUILDPLATFORM node:26.12.0-bookworm-slim@sha256:742a0fe7320e09e0321222b68165e6ac731d30470e7ea7f98f9bfc3a6e1034fe AS web
WORKDIR /web
RUN npm install --global --no-fund --no-audit pnpm@11.15.1
COPY web/ ./
RUN pnpm install --frozen-lockfile && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/.output/public ./internal/panel/dist
RUN mkdir /empty-data && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /burzblog ./cmd/burzblog

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL org.opencontainers.image.source="https://github.com/weburz/burzblog" \
      org.opencontainers.image.description="BurzBlog: write Markdown posts in the browser or the terminal"
COPY --from=build /burzblog /burzblog
# An empty /data owned by the runtime user: a new named volume copies this
# ownership, so the container can write without a --user override.
COPY --from=build --chown=65532:65532 /empty-data /data
# Compose overrides this with the numeric owner of the host data directory.
USER 65532:65532
ENV BURZBLOG_DATA=/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/burzblog", "healthcheck"]
ENTRYPOINT ["/burzblog"]
CMD ["serve", "--addr", "0.0.0.0:8080"]
