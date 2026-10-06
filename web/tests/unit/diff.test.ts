import { describe, expect, it } from "vitest";
import { diffTooLong, lineDiff } from "../../app/utils/diff";

describe("line diff", () => {
  it("marks identical text as all same", () => {
    expect(lineDiff("a\nb", "a\nb")).toEqual([
      { kind: "same", text: "a" },
      { kind: "same", text: "b" },
    ]);
  });
  it("reports a pure addition on disk", () => {
    expect(lineDiff("a\nc", "a\nb\nc")).toEqual([
      { kind: "same", text: "a" },
      { kind: "added", text: "b" },
      { kind: "same", text: "c" },
    ]);
  });
  it("reports a pure removal from the editor text", () => {
    expect(lineDiff("a\nb\nc", "a\nc")).toEqual([
      { kind: "same", text: "a" },
      { kind: "removed", text: "b" },
      { kind: "same", text: "c" },
    ]);
  });
  it("reports mixed changes", () => {
    expect(lineDiff("a\nmine\nc\nd", "a\ntheirs\nc\nd\ne")).toEqual([
      { kind: "same", text: "a" },
      { kind: "removed", text: "mine" },
      { kind: "added", text: "theirs" },
      { kind: "same", text: "c" },
      { kind: "same", text: "d" },
      { kind: "added", text: "e" },
    ]);
  });
  it("falls back to the disk lines beyond 4,000 lines", () => {
    const long = Array.from({ length: 4001 }, (_, index) => `line ${index}`).join("\n");
    expect(diffTooLong(long, "x")).toBe(true);
    expect(diffTooLong("x", "y")).toBe(false);
    const result = lineDiff("mine", long);
    expect(result).toHaveLength(4001);
    expect(result.every((line) => line.kind === "same")).toBe(true);
  });
});
