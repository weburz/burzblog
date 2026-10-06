export type DiffLine = { kind: "same" | "added" | "removed"; text: string };

export const diffLineLimit = 4000;

const lines = (value: string) => value.split("\n");

export const diffTooLong = (a: string, b: string): boolean =>
  lines(a).length > diffLineLimit || lines(b).length > diffLineLimit;

// Line diff of `a` (the editor text) against `b` (the disk text): lines only in `a` are
// "removed", lines only in `b` are "added". Common head and tail are trimmed first, then
// a longest-common-subsequence table covers the middle.
export const lineDiff = (a: string, b: string): DiffLine[] => {
  const left = lines(a);
  const right = lines(b);
  if (left.length > diffLineLimit || right.length > diffLineLimit)
    return right.map((text) => ({ kind: "same", text }));
  let head = 0;
  while (head < left.length && head < right.length && left[head] === right[head]) head++;
  let tail = 0;
  while (
    tail < left.length - head &&
    tail < right.length - head &&
    left[left.length - 1 - tail] === right[right.length - 1 - tail]
  )
    tail++;
  const x = left.slice(head, left.length - tail);
  const y = right.slice(head, right.length - tail);
  const width = y.length + 1;
  const table = new Uint16Array((x.length + 1) * width);
  for (let i = x.length - 1; i >= 0; i--)
    for (let j = y.length - 1; j >= 0; j--)
      table[i * width + j] =
        x[i] === y[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!);
  const result: DiffLine[] = left
    .slice(0, head)
    .map((text) => ({ kind: "same" as const, text }));
  let i = 0;
  let j = 0;
  while (i < x.length || j < y.length) {
    if (i < x.length && j < y.length && x[i] === y[j]) {
      result.push({ kind: "same", text: x[i]! });
      i++;
      j++;
    } else if (
      j >= y.length ||
      (i < x.length && table[(i + 1) * width + j]! >= table[i * width + j + 1]!)
    ) {
      result.push({ kind: "removed", text: x[i]! });
      i++;
    } else {
      result.push({ kind: "added", text: y[j]! });
      j++;
    }
  }
  for (const text of left.slice(left.length - tail)) result.push({ kind: "same", text });
  return result;
};
