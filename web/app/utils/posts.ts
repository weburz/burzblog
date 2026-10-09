// Filenames are ASCII only: letters, digits and single hyphens. Other characters
// (including non-ASCII letters) are dropped, not transliterated.
export const slugify = (title: string): string =>
  title
    .toLowerCase()
    .replace(/[^\p{ASCII}]/gu, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 128)
    .replace(/-+$/, "");

// The same lexical rules the server applies to content paths (storage.SEGMENT), so a
// move can explain a refusal before it is sent.
const segment = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
export const pathProblem = (path: string): string => {
  if (!path) return "Enter the new path, for example posts/my-post.md.";
  if (!path.endsWith(".md")) return "End the filename with .md.";
  const parts = path.split("/");
  if (parts.length > 32) return "Use at most 32 folder levels.";
  if (parts.some((part) => part === "." || part === ".." || !segment.test(part)))
    return "Use a path inside the content folder, like posts/my-post.md. Each folder and file name starts with a letter or number and uses only letters, numbers, dots, hyphens and underscores.";
  return "";
};
