// Filenames are ASCII only: letters, digits and single hyphens. Other characters
// (including non-ASCII letters) are dropped, not transliterated.
export const slugify = (title: string): string =>
  title
    .toLowerCase()
    .replace(/[^\p{ASCII}]/gu, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
