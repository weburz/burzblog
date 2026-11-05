import { describe, expect, it } from "vitest";
import type { TagCount } from "../../app/types";
import { suggestTags } from "../../app/utils/tags";

const tag = (name: string, count: number, label = name): TagCount => ({
  tag: name,
  count,
  label,
  custom: false,
});
const all = [
  tag("go", 5, "Go"),
  tag("golang", 2, "Golang"),
  tag("django", 9, "Django"),
  tag("constitutional-law", 4, "Constitutional Law"),
  tag("law", 1, "Legal"),
  tag("rust", 3, "Rust"),
];
const names = (list: TagCount[]) => list.map((t) => t.tag);

describe("suggestTags", () => {
  it("puts prefix matches before substring matches", () => {
    expect(names(suggestTags(all, "go", []))).toEqual(["go", "golang", "django"]);
  });

  it("orders each group by use, then name", () => {
    expect(names(suggestTags([tag("b", 1), tag("a", 1), tag("c", 7)], "", []))).toEqual([
      "c",
      "a",
      "b",
    ]);
  });

  it("leaves out tags already chosen", () => {
    expect(names(suggestTags(all, "go", ["go"]))).toEqual(["golang", "django"]);
  });

  it("matches the label as well as the slug", () => {
    expect(names(suggestTags(all, "legal", []))).toEqual(["law"]);
    expect(names(suggestTags(all, "Constitutional L", []))).toEqual([
      "constitutional-law",
    ]);
  });

  it("matches how the tag would be cleaned", () => {
    expect(names(suggestTags(all, "Constitutional-Law", []))).toEqual([
      "constitutional-law",
    ]);
  });

  it("offers the most used tags for nothing typed, up to the limit", () => {
    expect(names(suggestTags(all, "  ", ["django"], 2))).toEqual([
      "go",
      "constitutional-law",
    ]);
  });

  it("offers nothing for text no tag contains", () => {
    expect(suggestTags(all, "zzz", [])).toEqual([]);
    expect(suggestTags([], "go", [])).toEqual([]);
  });

  it("does not change the list it is given", () => {
    const copy = [...all];
    suggestTags(all, "", []);
    expect(all).toEqual(copy);
  });
});
