#!/bin/sh
# Add Umami analytics to every page of the built website, at deploy time only,
# so the website ID stays out of the repository and clones ship without it.
# The tracker only counts visits on the domain in site/CNAME (data-domains).
# Usage: UMAMI_WEBSITE_ID=<uuid> scripts/site-analytics.sh [site-dir]
set -eu
dir=${1:-site}
host=${UMAMI_HOST:-https://umami.weburz.com}
id=${UMAMI_WEBSITE_ID:-}
case $id in
  "") echo "site-analytics: UMAMI_WEBSITE_ID is not set; no analytics added" >&2; exit 0 ;;
esac
if ! printf '%s' "$id" | grep -Eq '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'; then
  echo "site-analytics: UMAMI_WEBSITE_ID must be the website's UUID from Umami" >&2
  exit 1
fi
if ! printf '%s' "$host" | grep -Eq '^https://[A-Za-z0-9.-]+$'; then
  echo "site-analytics: UMAMI_HOST must be https://<host> with no path" >&2
  exit 1
fi
domain=$(tr -d '[:space:]' < "$dir/CNAME")
tag="<script defer src=\"$host/script.js\" data-website-id=\"$id\" data-domains=\"$domain\"></script>"
count=0
for page in $(find "$dir" -name '*.html' | sort); do
  if ! grep -q "default-src 'none';" "$page" || ! grep -q '</head>' "$page"; then
    echo "site-analytics: $page has no CSP meta or </head>; cannot add analytics safely" >&2
    exit 1
  fi
  sed -i \
    -e "s#default-src 'none';#default-src 'none'; script-src $host; connect-src $host;#" \
    -e "s#</head>#  $tag\n  </head>#" \
    "$page"
  count=$((count + 1))
done
echo "site-analytics: added Umami to $count pages for $domain"
