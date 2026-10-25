import type { PostDraft } from "../types";

// Mirror the server's limits (internal/post).
export const maxTags = 20;
export const maxTagLength = 40;
export const maxAuthor = 100;
export const seoTitleLimits = { advised: 60, max: 120 };
export const seoDescriptionLimits = { advised: 160, max: 320 };

// Mirrors the server: lowercase letters, digits and marks, with single hyphens
// between words. "Machine Learning" becomes "machine-learning".
export const normalizeTag = (raw: string): string =>
  Array.from(
    raw
      .toLowerCase()
      .replace(/[^\p{L}\p{N}\p{M}]+/gu, "-")
      .replace(/^-+|-+$/g, ""),
  )
    .slice(0, maxTagLength)
    .join("")
    .replace(/-+$/, "");

// Normalizes each tag, drops empty ones and repeats, keeps the writer's order.
export const normalizeTags = (tags: readonly string[]): string[] => [
  ...new Set(tags.map(normalizeTag).filter(Boolean)),
];

// "42 / 60": how much of the advised length is used.
export const counterLabel = (text: string, advised: number): string =>
  `${Array.from(text).length} / ${advised}`;

export const overAdvice = (text: string, advised: number): boolean =>
  Array.from(text).length > advised;

// Canonical addresses must be absolute https URLs, or empty.
export const canonicalOk = (url: string): boolean =>
  url.trim() === "" || /^https:\/\/[^\s/]+\S*$/.test(url.trim());

const sameList = (a: readonly string[], b: readonly string[]): boolean =>
  a.length === b.length && a.every((v, i) => v === b[i]);

// Whether two drafts would save the same post. Missing text counts as empty
// and missing tags as none, so a backup from before these fields still compares.
export const sameDraft = (a: PostDraft, b: PostDraft): boolean =>
  (Object.keys(a) as (keyof PostDraft)[]).every((k) =>
    k === "tags" ? sameList(a.tags ?? [], b.tags ?? []) : (a[k] ?? "") === (b[k] ?? ""),
  );
