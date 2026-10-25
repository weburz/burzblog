import { describe, expect, it, vi } from "vitest";

vi.mock(
  "@fontsource-variable/sofia-sans/files/sofia-sans-latin-wght-normal.woff2?url",
  () => ({ default: "/sans.woff2" }),
);
vi.mock(
  "@fontsource-variable/sofia-sans/files/sofia-sans-latin-wght-italic.woff2?url",
  () => ({ default: "/sans-i.woff2" }),
);
vi.mock(
  "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2?url",
  () => ({ default: "/mono.woff2" }),
);

const { previewDocument } = await import("../../app/utils/preview");
const meta = { title: "", description: "", coverUrl: "", coverAlt: "" };

describe("previewDocument", () => {
  it("escapes the title, description and cover attributes", () => {
    const doc = previewDocument("<p>x</p>", {
      title: "<script>alert(1)</script>",
      description: 'a "quote"',
      coverUrl: 'https://x.test/a.png" onerror="alert(1)',
      coverAlt: "<b>",
    });
    expect(doc).not.toContain("<script>alert");
    expect(doc).toContain("&lt;script&gt;");
    expect(doc).toContain("&quot;quote&quot;");
    expect(doc).not.toContain('" onerror="');
  });
  it("only shows http(s) cover images", () => {
    expect(
      previewDocument("", { ...meta, coverUrl: "javascript:alert(1)" }),
    ).not.toContain("<img");
    expect(previewDocument("", { ...meta, coverUrl: "https://x.test/a.png" })).toContain(
      '<img class="cover"',
    );
  });
  it("blocks scripts with its own policy and invites writing when empty", () => {
    const doc = previewDocument("", meta);
    expect(doc).toContain("default-src 'none'");
    expect(doc).not.toMatch(/script-src/);
    expect(doc).toContain("Start writing on the left.");
    expect(doc).toContain("Untitled");
  });
});
