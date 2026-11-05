// Index of the last value that is at most target, or -1 when every value is
// larger. values must be sorted ascending.
export const lastAtMost = (values: number[], target: number): number => {
  let low = 0;
  let high = values.length - 1;
  let found = -1;
  while (low <= high) {
    const mid = (low + high) >> 1;
    if (values[mid]! <= target) {
      found = mid;
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  return found;
};

// The 1-based line of the text that offset points into.
export const lineAt = (text: string, offset: number): number => {
  let line = 1;
  for (let i = text.indexOf("\n"); i >= 0 && i < offset; i = text.indexOf("\n", i + 1)) {
    line++;
  }
  return line;
};

// The block the line belongs to: the one with the greatest start line that is
// at most the line, given every block's start line in order. A line above the
// first block gives -1.
export const blockAt = (starts: number[], line: number): number =>
  lastAtMost(starts, line);

// How far the preview must scroll so a block, given by its top and bottom
// edges relative to the viewport, is in view. Zero when it already is, or
// when a block taller than the viewport still fills part of it.
export const previewScroll = (top: number, bottom: number, height: number): number => {
  const margin = height * 0.15;
  if (bottom - top > height) return top < height && bottom > 0 ? 0 : top - height * 0.2;
  if (top >= 0 && bottom <= height) return 0;
  if (bottom <= 0 || top >= height) return top - height * 0.2;
  return top < 0 ? top - margin : bottom - height + margin;
};
