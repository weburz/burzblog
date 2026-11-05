import { describe, expect, it } from "vitest";
import { blockAt, lastAtMost, lineAt, previewScroll } from "../../app/utils/blocks";

describe("lastAtMost", () => {
  it("finds the last value not above the target", () => {
    expect(lastAtMost([1, 3, 8], 5)).toBe(1);
    expect(lastAtMost([1, 3, 8], 8)).toBe(2);
    expect(lastAtMost([1, 3, 8], 1)).toBe(0);
  });
  it("gives -1 when every value is larger, or there are none", () => {
    expect(lastAtMost([4, 6], 3)).toBe(-1);
    expect(lastAtMost([], 3)).toBe(-1);
  });
});

describe("lineAt", () => {
  const text = "ab\ncd\n\nef";
  it("counts lines from 1", () => {
    expect(lineAt(text, 0)).toBe(1);
    expect(lineAt(text, 2)).toBe(1);
    expect(lineAt(text, 3)).toBe(2);
    expect(lineAt(text, 6)).toBe(3);
    expect(lineAt(text, text.length)).toBe(4);
  });
  it("is 1 for empty text", () => {
    expect(lineAt("", 0)).toBe(1);
  });
});

describe("blockAt", () => {
  const starts = [1, 3, 6, 9];
  it("picks the block the line falls in", () => {
    expect(blockAt(starts, 1)).toBe(0);
    expect(blockAt(starts, 4)).toBe(1);
    expect(blockAt(starts, 9)).toBe(3);
    expect(blockAt(starts, 40)).toBe(3);
  });
  it("gives -1 above the first block", () => {
    expect(blockAt([5, 7], 2)).toBe(-1);
  });
});

describe("previewScroll", () => {
  it("leaves a visible block alone", () => {
    expect(previewScroll(100, 300, 600)).toBe(0);
  });
  it("brings a block that is off screen near the top", () => {
    expect(previewScroll(900, 1000, 600)).toBe(780);
    expect(previewScroll(-400, -300, 600)).toBe(-520);
  });
  it("reveals a block that is cut off", () => {
    expect(previewScroll(-50, 100, 600)).toBe(-140);
    expect(previewScroll(500, 700, 600)).toBe(190);
  });
  it("leaves a block taller than the view alone while it fills part of it", () => {
    expect(previewScroll(-200, 900, 600)).toBe(0);
    expect(previewScroll(1000, 2000, 600)).toBe(880);
  });
});
