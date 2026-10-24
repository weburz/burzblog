// Text edits for the Markdown toolbar and shortcuts. Each takes the source and
// the selection and returns the new source and selection, so they stay pure
// and testable; the editor applies them with execCommand to keep undo working.
export interface Edit {
  text: string;
  start: number;
  end: number;
}

// Wrap the selection in a marker ("**", "_", "`"), or unwrap it if it is
// already wrapped. With no selection, insert the markers around the cursor.
export const wrap = (
  source: string,
  start: number,
  end: number,
  marker: string,
): Edit => {
  const before = source.slice(start - marker.length, start);
  const after = source.slice(end, end + marker.length);
  if (before === marker && after === marker)
    return {
      text:
        source.slice(0, start - marker.length) +
        source.slice(start, end) +
        source.slice(end + marker.length),
      start: start - marker.length,
      end: end - marker.length,
    };
  const selected = source.slice(start, end);
  return {
    text: source.slice(0, start) + marker + selected + marker + source.slice(end),
    start: start + marker.length,
    end: end + marker.length,
  };
};

const lineStart = (source: string, index: number) =>
  source.lastIndexOf("\n", index - 1) + 1;

// Toggle a prefix ("## ", "- ", "> ", "1. ") on every line the selection
// touches. Headings replace an existing heading level instead of stacking.
export const prefixLines = (
  source: string,
  start: number,
  end: number,
  prefix: string,
): Edit => {
  const from = lineStart(source, start);
  // A selection that ends right after a newline does not include the next line.
  const last = end > start && source[end - 1] === "\n" ? end - 1 : end;
  const lineEnd = source.indexOf("\n", last);
  const to = lineEnd === -1 ? source.length : lineEnd;
  const block = source.slice(from, to);
  const lines = block.split("\n");
  const pattern = prefix.startsWith("#")
    ? /^#{1,6} /
    : prefix === "1. "
      ? /^\d+\. /
      : new RegExp(`^${prefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`);
  const allHave = lines.every((line) => line.startsWith(prefix));
  const next = lines.map((line, index) => {
    if (allHave) return line.slice(prefix.length);
    const bare = line.replace(pattern, "");
    return (prefix === "1. " ? `${index + 1}. ` : prefix) + bare;
  });
  const replaced = next.join("\n");
  const text = source.slice(0, from) + replaced + source.slice(to);
  const delta = replaced.length - block.length;
  return {
    text,
    start:
      start === end ? Math.max(from, start + (next[0]!.length - lines[0]!.length)) : from,
    end:
      start === end
        ? Math.max(from, end + (next[0]!.length - lines[0]!.length))
        : to + delta,
  };
};

// Turn the selection into a link, leaving the URL selected to type over.
export const link = (source: string, start: number, end: number): Edit => {
  const label = source.slice(start, end) || "link text";
  const url = "https://";
  const text = `${source.slice(0, start)}[${label}](${url})${source.slice(end)}`;
  const urlStart = start + label.length + 3;
  return { text, start: urlStart, end: urlStart + url.length };
};

// A fenced code block around the selection, or an empty one at the cursor.
export const codeBlock = (source: string, start: number, end: number): Edit => {
  const selected = source.slice(start, end);
  const needsBreak = start > 0 && source[start - 1] !== "\n" ? "\n" : "";
  const inserted = `${needsBreak}\`\`\`\n${selected}\n\`\`\`\n`;
  const text = source.slice(0, start) + inserted + source.slice(end);
  const inner = start + needsBreak.length + 4;
  return { text, start: inner, end: inner + selected.length };
};

const words = /[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu;

export const countWords = (text: string): number => text.match(words)?.length ?? 0;

// About 230 words a minute, rounded up, never "0 min".
export const readingMinutes = (wordCount: number): number =>
  Math.max(1, Math.ceil(wordCount / 230));
