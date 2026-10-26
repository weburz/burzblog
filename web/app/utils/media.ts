import type { MediaItem } from "../types";

// What the server accepts, for the file picker's accept attribute. The server
// decides by content, so this only narrows what the dialog offers.
export const acceptedTypes =
  "image/jpeg,image/png,image/gif,image/webp,image/avif,application/pdf";

export const isImage = (item: Pick<MediaItem, "mime">): boolean =>
  item.mime.startsWith("image/");

// "1.4 MB", "820 KB", "512 B".
export const sizeLabel = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

// "1200 × 800 · 1.4 MB" for images the server could measure.
export const detailLabel = (item: MediaItem): string =>
  [
    item.width && item.height ? `${item.width} × ${item.height}` : "",
    sizeLabel(item.size),
  ]
    .filter(Boolean)
    .join(" · ");

// Brackets and line breaks would end the alt text early.
const cleanLabel = (text: string): string =>
  text.replace(/[[\]]/g, "").replace(/\s+/g, " ").trim();

// The Markdown that shows an upload: an image with its alt text, or a link for
// a PDF.
export const markdownFor = (item: MediaItem): string =>
  isImage(item)
    ? `![${cleanLabel(item.alt)}](${item.url})`
    : `[${cleanLabel(item.originalName) || item.name}](${item.url})`;

// The files of a drop or a file input that are worth sending: directories and
// empty entries are skipped.
export const uploadable = (files: ArrayLike<File> | null | undefined): File[] =>
  Array.from(files ?? []).filter((file) => file.size > 0);
