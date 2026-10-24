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
