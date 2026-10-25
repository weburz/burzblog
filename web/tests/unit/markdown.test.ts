import { describe, expect, it } from "vitest";
import {
  codeBlock,
  countWords,
  lengthLabel,
  link,
  prefixLines,
  readingMinutes,
  wordLabel,
  wrap,
} from "../../app/utils/markdown";

describe("wrap", () => {
  it("wraps the selection and keeps it selected", () => {
    expect(wrap("say hello now", 4, 9, "**")).toEqual({
      text: "say **hello** now",
      start: 6,
      end: 11,
    });
  });
  it("unwraps an already wrapped selection", () => {
    expect(wrap("say **hello** now", 6, 11, "**")).toEqual({
      text: "say hello now",
      start: 4,
      end: 9,
    });
  });
  it("inserts empty markers at the cursor", () => {
    expect(wrap("ab", 1, 1, "_")).toEqual({ text: "a__b", start: 2, end: 2 });
  });
});

describe("prefixLines", () => {
  it("adds a prefix to every selected line", () => {
    expect(prefixLines("one\ntwo\nthree", 0, 7, "- ").text).toBe("- one\n- two\nthree");
  });
  it("removes the prefix when every line already has it", () => {
    expect(prefixLines("> a\n> b", 0, 7, "> ").text).toBe("a\nb");
  });
  it("replaces a heading level instead of stacking", () => {
    expect(prefixLines("### Title", 2, 2, "## ").text).toBe("## Title");
  });
  it("numbers ordered lists", () => {
    expect(prefixLines("a\nb\nc", 0, 5, "1. ").text).toBe("1. a\n2. b\n3. c");
  });
  it("keeps the cursor on its text", () => {
    const edit = prefixLines("hello", 3, 3, "## ");
    expect(edit.text).toBe("## hello");
    expect(edit.start).toBe(6);
  });
});

describe("link and code block", () => {
  it("links the selection and selects the URL", () => {
    const edit = link("see docs", 4, 8);
    expect(edit.text).toBe("see [docs](https://)");
    expect(edit.text.slice(edit.start, edit.end)).toBe("https://");
  });
  it("starts a code block on its own line", () => {
    const edit = codeBlock("text", 4, 4);
    expect(edit.text).toBe("text\n```\n\n```\n");
    expect(edit.start).toBe(9);
  });
});

describe("counting", () => {
  it("counts words in any script, ignoring Markdown symbols", () => {
    expect(countWords("## Hello, *world* — it's 2026! Привет")).toBe(5);
    expect(countWords("")).toBe(0);
  });
  it("never says zero minutes", () => {
    expect(readingMinutes(0)).toBe(1);
    expect(readingMinutes(231)).toBe(2);
  });
});

describe("wordLabel", () => {
  it("uses the singular for one word", () => {
    expect(wordLabel(1)).toBe("1 word");
  });
  it("groups digits for larger counts", () => {
    expect(wordLabel(0)).toBe("0 words");
    expect(wordLabel(1204)).toBe("1,204 words");
  });
});

describe("lengthLabel", () => {
  it("has no reading time for an empty post", () => {
    expect(lengthLabel(0)).toBe("No words yet");
  });
  it("adds the reading time otherwise", () => {
    expect(lengthLabel(1)).toBe("1 word, 1 min read");
    expect(lengthLabel(460)).toBe("460 words, 2 min read");
  });
});
