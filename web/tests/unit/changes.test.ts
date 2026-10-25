import { describe, expect, it } from "vitest";
import type { RevisionState } from "../../app/types";
import { changeSummary } from "../../app/utils/changes";

const state = (over: Partial<RevisionState> = {}): RevisionState => ({
  slug: "hello",
  title: "Hello",
  status: "draft",
  words: 100,
  descriptionDigest: "d1",
  coverDigest: "c1",
  settingsDigest: "s1",
  ...over,
});

describe("changeSummary", () => {
  it("reports word gains and losses with a real minus sign", () => {
    expect(changeSummary(state(), state({ words: 142 }))).toBe("+42 words");
    expect(changeSummary(state(), state({ words: 97 }))).toBe("−3 words");
    expect(changeSummary(state(), state({ words: 101 }))).toBe("+1 word");
  });

  it("names title, slug, cover, description and settings changes", () => {
    expect(changeSummary(state(), state({ title: "Hi" }))).toBe("Title changed");
    expect(changeSummary(state(), state({ slug: "new-slug" }))).toBe(
      "Renamed to new-slug",
    );
    expect(changeSummary(state(), state({ coverDigest: "c2" }))).toBe("Cover changed");
    expect(changeSummary(state(), state({ descriptionDigest: "d2" }))).toBe(
      "Description changed",
    );
    expect(changeSummary(state(), state({ settingsDigest: "s2" }))).toBe(
      "Settings changed",
    );
  });

  it("names status transitions", () => {
    expect(changeSummary(state(), state({ status: "published" }))).toBe("Published");
    expect(
      changeSummary(state({ status: "published" }), state({ status: "draft" })),
    ).toBe("Unpublished");
    expect(changeSummary(state(), state({ status: "archived" }))).toBe("Archived");
    expect(changeSummary(state({ status: "archived" }), state({ status: "draft" }))).toBe(
      "Restored",
    );
  });

  it("combines several changes, words last", () => {
    expect(changeSummary(state(), state({ title: "Hi", words: 112 }))).toBe(
      "Title changed, +12 words",
    );
  });

  it("says so when nothing visible changed", () => {
    expect(changeSummary(state(), state())).toBe("No visible change");
  });
});
