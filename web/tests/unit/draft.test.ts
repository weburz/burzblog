import { describe, expect, it } from "vitest";
import type { Post, PostDraft } from "../../app/types";
import { ApiError } from "../../app/utils/api";
import { isEmptyDraft, isTransientFailure } from "../../app/utils/draft";

const post = (change: Partial<Post> = {}): Post => ({
  slug: "untitled",
  title: "",
  description: "",
  status: "draft",
  body: "",
  coverUrl: "",
  coverAlt: "",
  extra: "",
  tags: [],
  author: "ada",
  seoTitle: "",
  seoDescription: "",
  canonicalUrl: "",
  featured: false,
  publishedOn: null,
  updatedOn: null,
  publishAt: null,
  createdAt: "2026-11-05T10:00:00Z",
  updatedAt: "2026-11-05T10:00:00Z",
  updatedBy: "ada",
  version: 1,
  ...change,
});
const draftOf = (p: Post, change: Partial<PostDraft> = {}): PostDraft => ({
  slug: p.slug,
  title: p.title,
  description: p.description,
  body: p.body,
  coverUrl: p.coverUrl,
  coverAlt: p.coverAlt,
  extra: p.extra,
  tags: [...p.tags],
  author: p.author,
  seoTitle: p.seoTitle,
  seoDescription: p.seoDescription,
  canonicalUrl: p.canonicalUrl,
  featured: p.featured,
  publishedOn: p.publishedOn,
  updatedOn: p.updatedOn,
  ...change,
});

describe("isEmptyDraft", () => {
  it("is true for a fresh draft, whitespace included", () => {
    const p = post();
    expect(isEmptyDraft(p, draftOf(p))).toBe(true);
    expect(
      isEmptyDraft(
        p,
        draftOf(p, { title: "  ", body: "\n\t", description: " ", extra: " " }),
      ),
    ).toBe(true);
  });

  it.each<[string, Partial<PostDraft>]>([
    ["title", { title: "T" }],
    ["body", { body: "words" }],
    ["description", { description: "d" }],
    ["tags", { tags: ["go"] }],
    ["cover", { coverUrl: "/media/a.png" }],
    ["cover alt", { coverAlt: "alt" }],
    ["seo title", { seoTitle: "s" }],
    ["seo description", { seoDescription: "s" }],
    ["canonical address", { canonicalUrl: "https://example.com/a" }],
    ["extra", { extra: "layout: wide" }],
    ["another author", { author: "Grace" }],
    ["featured", { featured: true }],
  ])("is false when the editor holds a %s", (_, change) => {
    const p = post();
    expect(isEmptyDraft(p, draftOf(p, change))).toBe(false);
  });

  it("is false when only the saved post holds metadata", () => {
    const p = post({ description: "kept", tags: ["a"] });
    expect(isEmptyDraft(p, draftOf(post()))).toBe(false);
  });

  it("is false for posts that were published or are not drafts", () => {
    expect(isEmptyDraft(post({ status: "archived" }), draftOf(post()))).toBe(false);
    expect(
      isEmptyDraft(post({ publishedOn: "2026-11-01T00:00:00Z" }), draftOf(post())),
    ).toBe(false);
  });
});

describe("isTransientFailure", () => {
  it.each([0, 401, 500, 503])("retries a %i", (status) => {
    expect(isTransientFailure(new ApiError(status, "x", "m"))).toBe(true);
  });
  it.each([404, 409, 422])("does not retry a %i", (status) => {
    expect(isTransientFailure(new ApiError(status, "x", "m"))).toBe(false);
  });
  it("does not retry something that is not an API error", () => {
    expect(isTransientFailure(new Error("boom"))).toBe(false);
  });
});
