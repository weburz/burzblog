// Mirrors the server's slug rules: lowercase letters, digits and single
// hyphens between words, at most 120 characters.
export const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

// Make a slug from free text. While the writer is typing a slug, keep a
// trailing hyphen so "my-" can become "my-post".
export const slugify = (text: string, typing = false): string => {
  const slug = text
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+/, "")
    .slice(0, 120);
  return typing ? slug.replace(/-{2,}/g, "-") : slug.replace(/-+$/, "");
};

// The slug of a draft the server named "untitled", "untitled-2", ...
export const untitledPattern = /^untitled(?:-\d+)?$/;

// Whether slug is what a post with this title gets: the slugified title, with
// a number after it when another post already had that slug. An empty title
// gets "untitled". The editor keeps a draft's slug following its title for as
// long as it matches.
export const matchesTitle = (slug: string, title: string): boolean => {
  const base = slugify(title) || "untitled";
  return slug === base || new RegExp(`^${base}-\\d+$`).test(slug);
};
