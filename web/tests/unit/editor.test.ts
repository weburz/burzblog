import { describe, expect, it } from "vitest";
import { editorText, savedSource, previewDocument } from "../../app/utils/editor";

describe("source fidelity", () => {
  it("preserves CRLF and missing final newline on a no-op", () => {
    const source = "---\r\ntitle: 'Quoted' # custom\r\n---\r\nbody";
    expect(savedSource(source, editorText(source))).toBe(source);
  });
  it("accepts deliberate source edits without inventing serialization", () => {
    expect(savedSource("old\n", "new\n")).toBe("new\n");
  });
  it("isolates previews with a deny-by-default policy", () => {
    expect(previewDocument("<h1>Hello</h1>")).toContain("default-src 'none'");
  });
});
