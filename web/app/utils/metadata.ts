import type { PostDraft } from "../types";

// Mirror the server's limits (internal/post).
export const maxTags = 20;
export const maxTagLength = 40;
export const maxAuthor = 100;
export const maxTagLabel = 60;
export const seoTitleLimits = { advised: 60, max: 120 };
export const seoDescriptionLimits = { advised: 160, max: 320 };

// Mirrors the server: composed to NFC, lowercase letters, digits and marks, with
// single hyphens between words. "Machine Learning" becomes "machine-learning".
// The length limit is not applied here, so callers can tell a long tag from a cut one.
export const tagSlug = (raw: string): string =>
  raw
    .normalize("NFC")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}]+/gu, "-")
    .replace(/^-+|-+$/g, "");

export const normalizeTag = (raw: string): string =>
  Array.from(tagSlug(raw)).slice(0, maxTagLength).join("").replace(/-+$/, "");

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

// Whether two drafts would save the same post. Missing text counts as empty,
// missing tags as none and a missing featured flag as off, so a backup from
// before these fields still compares.
export const sameDraft = (a: PostDraft, b: PostDraft): boolean =>
  (Object.keys(a) as (keyof PostDraft)[]).every((k) => {
    if (k === "tags") return sameList(a.tags ?? [], b.tags ?? []);
    if (k === "featured") return Boolean(a.featured) === Boolean(b.featured);
    return (a[k] ?? "") === (b[k] ?? "");
  });
