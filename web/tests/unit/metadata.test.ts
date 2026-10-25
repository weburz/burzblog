import { describe, expect, it } from "vitest";
import {
  canonicalOk,
  counterLabel,
  maxTagLength,
  normalizeTag,
  normalizeTags,
  overAdvice,
  sameDraft,
} from "../../app/utils/metadata";
import type { PostDraft } from "../../app/types";

const draft = (change: Partial<PostDraft> = {}): PostDraft => ({
  slug: "hello",
  title: "Hello",
  description: "",
  body: "",
  coverUrl: "",
  coverAlt: "",
  extra: "",
  tags: ["go"],
  author: "ada",
  seoTitle: "",
  seoDescription: "",
  canonicalUrl: "",
  publishedOn: null,
  updatedOn: null,
  ...change,
});

describe("normalizeTag", () => {
  it.each([
    ["Go", "go"],
    ["  Machine Learning ", "machine-learning"],
    ["C++", "c"],
    ["web--dev", "web-dev"],
    ["-edge-", "edge"],
    ["2026", "2026"],
    ["Über Café", "über-café"],
    ["!!!", ""],
    ["", ""],
  ])("%j → %j", (input, tag) => {
    expect(normalizeTag(input)).toBe(tag);
  });
  it("stops at the server's length limit without a dangling hyphen", () => {
    const tag = normalizeTag(`${"a".repeat(maxTagLength - 1)} b`);
    expect(tag).toBe("a".repeat(maxTagLength - 1));
    expect(Array.from(normalizeTag("x".repeat(100))).length).toBe(maxTagLength);
  });
});

describe("normalizeTags", () => {
  it("cleans, dedupes and keeps the writer's order", () => {
    expect(normalizeTags(["Go", "web dev", "go", "", "WEB-DEV", "rust"])).toEqual([
      "go",
      "web-dev",
      "rust",
    ]);
  });
  it("accepts an empty list", () => {
    expect(normalizeTags([])).toEqual([]);
  });
});

describe("counters", () => {
  it("counts characters, not UTF-16 units", () => {
    expect(counterLabel("héllo 👋", 60)).toBe("7 / 60");
    expect(counterLabel("", 160)).toBe("0 / 160");
  });
  it("flags text past the advised length only", () => {
    expect(overAdvice("a".repeat(60), 60)).toBe(false);
    expect(overAdvice("a".repeat(61), 60)).toBe(true);
  });
});

describe("canonicalOk", () => {
  it.each(["", "  ", "https://example.com", "https://example.com/blog/post?x=1"])(
    "accepts %j",
    (url) => expect(canonicalOk(url)).toBe(true),
  );
  it.each([
    "http://example.com/a",
    "/relative",
    "example.com/a",
    "https://",
    "https:///a",
    "javascript:alert(1)",
    "https://exa mple.com",
  ])("refuses %j", (url) => expect(canonicalOk(url)).toBe(false));
});

describe("sameDraft", () => {
  it("is equal for equal drafts, whatever the array identity", () => {
    expect(sameDraft(draft(), draft())).toBe(true);
  });
  it("sees tag changes, including order", () => {
    expect(sameDraft(draft(), draft({ tags: ["go", "web"] }))).toBe(false);
    expect(sameDraft(draft({ tags: ["a", "b"] }), draft({ tags: ["b", "a"] }))).toBe(
      false,
    );
  });
  it("sees SEO, author and canonical changes", () => {
    for (const change of [
      { author: "grace" },
      { seoTitle: "t" },
      { seoDescription: "d" },
      { canonicalUrl: "https://example.com" },
    ])
      expect(sameDraft(draft(), draft(change))).toBe(false);
  });
  it("treats a backup from before these fields as having none", () => {
    const old = draft();
    delete (old as Partial<PostDraft>).tags;
    expect(sameDraft(old, draft({ tags: [] }))).toBe(true);
  });
});
