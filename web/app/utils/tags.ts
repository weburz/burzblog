import type { TagCount } from "../types";
import { maxTagLength, maxTags, normalizeTag, tagSlug } from "./metadata";

export const maxSuggestions = 8;

// The existing tags to offer for what the writer has typed: tags that start
// with it first, then tags that contain it, most used first within each group.
// A tag matches by its slug or by its label. Tags already chosen are left out;
// nothing typed offers the most used tags.
export const suggestTags = (
  all: readonly TagCount[],
  query: string,
  chosen: readonly string[],
  limit = maxSuggestions,
): TagCount[] => {
  const taken = new Set(chosen);
  const text = query.trim().toLowerCase();
  const slug = normalizeTag(query);
  const byUse = (a: TagCount, b: TagCount) =>
    b.count - a.count || (a.tag < b.tag ? -1 : 1);
  const open = all.filter((t) => !taken.has(t.tag));
  if (!text) return open.sort(byUse).slice(0, limit);
  const prefix: TagCount[] = [];
  const inside: TagCount[] = [];
  for (const t of open) {
    const label = t.label.toLowerCase();
    if ((slug && t.tag.startsWith(slug)) || label.startsWith(text)) prefix.push(t);
    else if ((slug && t.tag.includes(slug)) || label.includes(text)) inside.push(t);
  }
  return [...prefix.sort(byUse), ...inside.sort(byUse)].slice(0, limit);
};

// Why a typed tag cannot be added, or "" when it can. A tag that is already
// chosen is no problem: adding it again changes nothing.
export const tagLimitProblem = (chosen: readonly string[], raw: string): string => {
  const slug = tagSlug(raw);
  if (!slug) return "";
  if (Array.from(slug).length > maxTagLength)
    return `Tags are at most ${maxTagLength} characters`;
  if (!chosen.includes(normalizeTag(raw)) && chosen.length >= maxTags)
    return `A post can carry at most ${maxTags} tags`;
  return "";
};
