import { describe, expect, it } from "vitest";
import {
  codeBlock,
  continueList,
  countWords,
  horizontalRule,
  image,
  imageUrlError,
  indentList,
  lengthLabel,
  link,
  prefixLines,
  readingMinutes,
  table,
  taskPrefix,
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

describe("task lists", () => {
  it("prefixes lines with an empty checkbox", () => {
    expect(prefixLines("one\ntwo", 0, 7, taskPrefix).text).toBe("- [ ] one\n- [ ] two");
  });
  it("replaces a bullet or number instead of stacking", () => {
    expect(prefixLines("- one\n2. two", 0, 12, taskPrefix).text).toBe(
      "- [ ] one\n- [ ] two",
    );
  });
  it("removes the checkbox when every line has it", () => {
    expect(prefixLines("- [ ] one\n- [ ] two", 0, 19, taskPrefix).text).toBe("one\ntwo");
  });
});

describe("imageUrlError", () => {
  it("accepts https and http addresses", () => {
    expect(imageUrlError("https://example.com/a.png")).toBe("");
    expect(imageUrlError(" http://example.com/a.png ")).toBe("");
  });
  it("rejects empty, relative and other schemes", () => {
    expect(imageUrlError("")).not.toBe("");
    expect(imageUrlError("/a.png")).not.toBe("");
    expect(imageUrlError("javascript:alert(1)")).not.toBe("");
    expect(imageUrlError("data:image/png;base64,AAAA")).not.toBe("");
    expect(imageUrlError("ftp://example.com/a.png")).not.toBe("");
  });
});

describe("image", () => {
  it("inserts markdown at the cursor and leaves the cursor after it", () => {
    const edit = image("ab", 1, 1, "A cat", "https://x.test/c.png");
    expect(edit.text).toBe("a![A cat](https://x.test/c.png)b");
    expect(edit.start).toBe(edit.text.indexOf("b"));
    expect(edit.end).toBe(edit.start);
  });
  it("replaces the selection and strips brackets from the alt text", () => {
    expect(image("see WORD", 4, 8, "a [b]", " https://x.test/c.png ").text).toBe(
      "see ![a b](https://x.test/c.png)",
    );
  });
});

describe("table", () => {
  it("inserts a skeleton and selects the first header cell", () => {
    const edit = table("", 0, 0);
    expect(edit.text).toBe("| Column 1 | Column 2 |\n| --- | --- |\n|  |  |\n");
    expect(edit.text.slice(edit.start, edit.end)).toBe("Column 1");
  });
  it("starts on its own paragraph after text", () => {
    const edit = table("intro", 5, 5);
    expect(edit.text.startsWith("intro\n\n| Column 1")).toBe(true);
    expect(edit.text.slice(edit.start, edit.end)).toBe("Column 1");
  });
  it("keeps the text that followed", () => {
    expect(table("a\n\nb", 3, 3).text.endsWith("|  |  |\n\nb")).toBe(true);
  });
});

describe("horizontalRule", () => {
  it("separates the rule from the paragraph above so it is not a heading", () => {
    const edit = horizontalRule("Some text", 9, 9);
    expect(edit.text).toBe("Some text\n\n---\n");
    expect(edit.start).toBe(edit.text.indexOf("---") + 4);
  });
  it("does not add extra blank lines that are already there", () => {
    expect(horizontalRule("a\n\nb", 3, 3).text).toBe("a\n\n---\n\nb");
  });
});

describe("continueList", () => {
  const at = (source: string) => continueList(source, source.length, source.length);
  it("continues bullets with the same marker", () => {
    expect(at("- one")).toEqual({ text: "- one\n- ", start: 8, end: 8 });
    expect(at("* one")?.text).toBe("* one\n* ");
  });
  it("increments ordered numbers", () => {
    expect(at("1. one")?.text).toBe("1. one\n2. ");
    expect(at("9. nine")?.text).toBe("9. nine\n10. ");
  });
  it("continues task lists with an unchecked box", () => {
    expect(at("- [x] done")?.text).toBe("- [x] done\n- [ ] ");
    expect(at("- [ ] todo")?.text).toBe("- [ ] todo\n- [ ] ");
  });
  it("keeps the indent of nested items", () => {
    expect(at("- a\n  - b")?.text).toBe("- a\n  - b\n  - ");
  });
  it("ends the list on an empty item", () => {
    expect(at("- one\n- ")).toEqual({ text: "- one\n", start: 6, end: 6 });
    expect(at("1. one\n2. ")?.text).toBe("1. one\n");
    expect(at("- [ ] ")?.text).toBe("");
  });
  it("leaves other lines and mid-line cursors alone", () => {
    expect(at("plain text")).toBeNull();
    expect(at("---")).toBeNull();
    expect(at("1.5 million")).toBeNull();
    expect(continueList("- one two", 5, 5)).toBeNull();
    expect(continueList("- one two", 2, 5)).toBeNull();
  });
  it("continues between items when the cursor ends a middle item", () => {
    expect(continueList("- a\n- b", 3, 3)?.text).toBe("- a\n- \n- b");
  });
});

describe("indentList", () => {
  it("indents the item by two spaces and keeps the cursor in place", () => {
    expect(indentList("- a\n- b", 7, 7, false)).toEqual({
      text: "- a\n  - b",
      start: 9,
      end: 9,
    });
  });
  it("outdents by up to two spaces", () => {
    expect(indentList("- a\n    - b", 11, 11, true)?.text).toBe("- a\n  - b");
    expect(indentList("- a\n - b", 8, 8, true)?.text).toBe("- a\n- b");
  });
  it("shifts every list line in the selection", () => {
    expect(indentList("- a\n- b\n- c", 0, 11, false)?.text).toBe("  - a\n  - b\n  - c");
  });
  it("returns null outside a list so Tab keeps moving focus", () => {
    expect(indentList("plain", 2, 2, false)).toBeNull();
    expect(indentList("plain", 2, 2, true)).toBeNull();
  });
  it("returns null when there is nothing to outdent", () => {
    expect(indentList("- a", 3, 3, true)).toBeNull();
  });
});
