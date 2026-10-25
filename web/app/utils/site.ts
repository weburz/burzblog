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

// A curl line that downloads one site file with a build token.
export const fetchCommand = (origin: string, file: SiteFile): string =>
  `curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \\\n  ${origin}${file.path} -o ${file.name}`;
