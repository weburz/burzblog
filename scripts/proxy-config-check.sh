#!/usr/bin/env bash
# Validate the shipped reverse-proxy examples with pinned Caddy and nginx images.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
caddy_image="caddy:2.11-alpine@sha256:881bbc60f9986d5ab8e7cfd6cf7e4ef3c9c0439fef2429d035d065577882f028"
nginx_image="nginx:1.30.0-alpine@sha256:0272e4604ed93c1792f03695a033a6e8546840f86e0de20a884bb17d2c924883"

docker run --rm --network none \
  --mount "type=bind,source=$root/deploy/Caddyfile,target=/etc/caddy/Caddyfile,readonly" \
  "$caddy_image" sh -ec '
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>/dev/null
    caddy fmt /etc/caddy/Caddyfile | diff -u /etc/caddy/Caddyfile -'
printf 'deploy/Caddyfile: valid and caddy-fmt clean\n'

# nginx -t needs the referenced certificate files; mount throwaway ones.
certificates="$(mktemp -d)"
trap 'rm -rf "$certificates"' EXIT
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -days 1 -subj /CN=blog.example.org -keyout "$certificates/privkey.pem" \
  -out "$certificates/fullchain.pem" 2>/dev/null
chmod 0644 "$certificates/privkey.pem"
docker run --rm --network none \
  --mount "type=bind,source=$root/deploy/nginx.conf,target=/etc/nginx/conf.d/default.conf,readonly" \
  --mount "type=bind,source=$certificates,target=/etc/letsencrypt/live/blog.example.org,readonly" \
  "$nginx_image" nginx -t
printf 'deploy/nginx.conf: nginx -t passed\n'
