import { describe, expect, it } from "vitest";
import { examplePostUrl, fetchCommand, siteFiles } from "../../app/utils/site";
import type { SiteSettings } from "../../app/types";

const configured: SiteSettings = {
  configured: true,
  url: "https://example.com",
  postPath: "/blog/{slug}",
  tagPath: "",
  feeds: [
    "/api/content/feed.xml",
    "/api/content/feed.atom",
    "/api/content/feed.json",
    "/api/content/sitemap.xml",
  ],
};

describe("siteFiles", () => {
  it("names each route by its file", () => {
    expect(siteFiles(configured).map((f) => f.name)).toEqual([
      "feed.xml",
      "feed.atom",
      "feed.json",
      "sitemap.xml",
    ]);
  });

  it("is empty when no site URL is set", () => {
    expect(siteFiles({ configured: false, feeds: [] })).toEqual([]);
  });
});

describe("examplePostUrl", () => {
  it("fills the post path", () => {
    expect(examplePostUrl(configured)).toBe("https://example.com/blog/my-post");
  });

  it("follows a custom post path", () => {
    expect(examplePostUrl({ ...configured, postPath: "/notes/{slug}/" })).toBe(
      "https://example.com/notes/my-post/",
    );
  });
});

describe("fetchCommand", () => {
  it("downloads the file with a build token", () => {
    const [feed] = siteFiles(configured);
    expect(fetchCommand("https://blog.example.com", feed!)).toBe(
      'curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \\\n  https://blog.example.com/api/content/feed.xml -o feed.xml',
    );
  });
});
