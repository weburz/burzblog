import type { SiteSettings } from "../types";

export interface SiteFile {
  name: string;
  path: string;
}

// The feed and sitemap routes a website build can fetch, by file name.
export const siteFiles = (site: SiteSettings): SiteFile[] =>
  site.feeds.map((path) => ({ name: path.split("/").pop() ?? path, path }));

// "https://example.com/blog/my-post": where a post would live on the website.
export const examplePostUrl = (site: SiteSettings): string =>
  (site.url ?? "") + (site.postPath ?? "").replace("{slug}", "my-post");

// The public address of a post: the template from the session with {slug}
// filled in. Empty when no site URL is configured.
export const publicPostUrl = (template: string, slug: string): string =>
  template && slug ? template.replace("{slug}", encodeURIComponent(slug)) : "";

// The toast for "Copy link" or "View on site" when no website address is set.
export const noSiteUrlNotice = {
  title: "No website address set",
  description:
    "Set BURZBLOG_SITE_URL (or --site-url) and restart the server. BURZBLOG_POST_PATH changes the path.",
  color: "neutral" as const,
};

// A curl line that downloads one site file with a build token.
export const fetchCommand = (origin: string, file: SiteFile): string =>
  `curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \\\n  ${origin}${file.path} -o ${file.name}`;
