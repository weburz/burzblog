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

export const taskPrefix = "- [ ] ";

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
      : prefix === taskPrefix
        ? /^(?:[-*] (?:\[[ xX]\] )?|\d+\. )/
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

// Only http(s) images are allowed; uploaded images are inserted by their URL.
// Returns a message for the writer, or "" when the URL is fine.
export const imageUrlError = (url: string): string => {
  const value = url.trim();
  if (!value) return "Enter the image address.";
  try {
    const { protocol } = new URL(value);
    if (protocol === "https:" || protocol === "http:") return "";
  } catch {
    // Falls through to the message below.
  }
  return "Use a full address that starts with https:// or http://.";
};

// An image with alt text, replacing the selection.
export const image = (
  source: string,
  start: number,
  end: number,
  alt: string,
  url: string,
): Edit => {
  const inserted = `![${alt.replace(/[[\]]/g, "").replace(/\s+/g, " ").trim()}](${url.trim()})`;
  const caret = start + inserted.length;
  return {
    text: source.slice(0, start) + inserted + source.slice(end),
    start: caret,
    end: caret,
  };
};

// Insert a block (table, rule) on its own lines with blank lines around it, so
// a rule never turns the paragraph above into a heading.
const insertBlock = (
  source: string,
  start: number,
  end: number,
  block: string,
): { text: string; offset: number } => {
  const before = source.slice(0, start);
  const after = source.slice(end);
  const lead = !before
    ? ""
    : before.endsWith("\n\n")
      ? ""
      : before.endsWith("\n")
        ? "\n"
        : "\n\n";
  const trail = after && !after.startsWith("\n") ? "\n" : "";
  return {
    text: `${before}${lead}${block}\n${trail}${after}`,
    offset: before.length + lead.length,
  };
};

// A two-column table skeleton with the first header cell selected to type over.
export const table = (source: string, start: number, end: number): Edit => {
  const header = "Column 1";
  const block = `| ${header} | Column 2 |\n| --- | --- |\n|  |  |`;
  const { text, offset } = insertBlock(source, start, end, block);
  return { text, start: offset + 2, end: offset + 2 + header.length };
};

export const horizontalRule = (source: string, start: number, end: number): Edit => {
  const { text, offset } = insertBlock(source, start, end, "---");
  const caret = offset + 4;
  return { text, start: caret, end: caret };
};

const listItem = /^(\s*)([-*+]|\d+\.)( \[[ xX]\])? (.*)$/;
const quoteLine = /^(\s*)> ?(.*)$/;

// Whether the cursor sits inside an open fenced code block, where list and
// quote markers are code and must stay untouched.
const inFence = (source: string, index: number): boolean => {
  const lines = source.slice(0, lineStart(source, index)).split("\n");
  return lines.filter((line) => /^\s{0,3}(```|~~~)/.test(line)).length % 2 === 1;
};

// Remove an empty list or quote line. A blank line is left behind so what the
// writer types next is a new paragraph, not a continuation of the item.
const leaveBlock = (source: string, from: number, to: number): Edit => {
  const before = source.slice(0, from);
  const gap = before && !before.endsWith("\n\n") ? "\n" : "";
  const caret = from + gap.length;
  return { text: before + gap + source.slice(to), start: caret, end: caret };
};

// Enter in a list item or quote: continue it, or end it when the item is empty
// (an empty nested item moves out one level first). Returns null when Enter
// should behave normally (not at the end of a list item, or inside code).
export const continueList = (source: string, start: number, end: number): Edit | null => {
  if (start !== end) return null;
  const from = lineStart(source, start);
  const found = source.indexOf("\n", start);
  const to = found === -1 ? source.length : found;
  if (to !== start || inFence(source, start)) return null;
  const line = source.slice(from, to);
  const quote = !listItem.test(line) ? quoteLine.exec(line) : null;
  if (quote) {
    const [, indent, content] = quote as unknown as [string, string, string];
    if (!content.trim()) return leaveBlock(source, from, to);
    const inserted = `\n${indent}> `;
    const caret = start + inserted.length;
    return {
      text: source.slice(0, start) + inserted + source.slice(end),
      start: caret,
      end: caret,
    };
  }
  const match = listItem.exec(line);
  if (!match) return null;
  const [, indent, marker, task, content] = match as unknown as [
    string,
    string,
    string,
    string | undefined,
    string,
  ];
  if (!content.trim()) {
    if (indent.length === 0) return leaveBlock(source, from, to);
    const kept = indent.slice(Math.min(2, indent.length));
    const text = `${source.slice(0, from)}${kept}${line.slice(indent.length)}${source.slice(to)}`;
    const caret = from + kept.length + line.length - indent.length;
    return { text, start: caret, end: caret };
  }
  const next = /^\d+\.$/.test(marker) ? `${Number.parseInt(marker, 10) + 1}.` : marker;
  const inserted = `\n${indent}${next}${task ? " [ ]" : ""} `;
  const caret = start + inserted.length;
  return {
    text: source.slice(0, start) + inserted + source.slice(end),
    start: caret,
    end: caret,
  };
};

// Enter inside a fenced code block keeps the indentation of the line above, so
// nested code does not have to be re-indented by hand. Null elsewhere.
export const keepIndent = (source: string, start: number, end: number): Edit | null => {
  if (!inFence(source, start)) return null;
  const from = lineStart(source, start);
  const indent = /^[ \t]*/.exec(source.slice(from, start))![0];
  if (!indent) return null;
  const inserted = `\n${indent}`;
  const caret = start + inserted.length;
  return {
    text: source.slice(0, start) + inserted + source.slice(end),
    start: caret,
    end: caret,
  };
};

// Tab and Shift+Tab in a list item move it by two spaces, for every line the
// selection touches. Returns null when the cursor is not in a list item, or
// when there is nothing left to outdent, so the browser keeps its focus order.
export const indentList = (
  source: string,
  start: number,
  end: number,
  outdent: boolean,
): Edit | null => {
  const from = lineStart(source, start);
  const last = end > start && source[end - 1] === "\n" ? end - 1 : end;
  const found = source.indexOf("\n", last);
  const to = found === -1 ? source.length : found;
  const lines = source.slice(from, to).split("\n");
  if (!listItem.test(lines[0]!) || inFence(source, start)) return null;
  let changed = false;
  const shifts: number[] = [];
  const next = lines.map((line) => {
    if (!listItem.test(line)) {
      shifts.push(0);
      return line;
    }
    const removed = outdent ? Math.min(2, /^ */.exec(line)![0].length) : 0;
    const result = outdent ? line.slice(removed) : `  ${line}`;
    if (result !== line) changed = true;
    shifts.push(outdent ? -removed : 2);
    return result;
  });
  if (!changed) return null;
  const total = shifts.reduce((sum, n) => sum + n, 0);
  return {
    text: source.slice(0, from) + next.join("\n") + source.slice(to),
    start: Math.max(from, start + shifts[0]!),
    end: Math.max(from, end + total),
  };
};

const words = /[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu;

export const countWords = (text: string): number => text.match(words)?.length ?? 0;

// About 230 words a minute, rounded up, never "0 min".
export const readingMinutes = (wordCount: number): number =>
  Math.max(1, Math.ceil(wordCount / 230));

// "1 word", "1,204 words": counts as people read them.
export const wordLabel = (wordCount: number): string =>
  wordCount === 1 ? "1 word" : `${wordCount.toLocaleString()} words`;

// The editor's length summary; an empty post has no reading time.
export const lengthLabel = (wordCount: number): string =>
  wordCount === 0
    ? "No words yet"
    : `${wordLabel(wordCount)}, ${readingMinutes(wordCount)} min read`;
