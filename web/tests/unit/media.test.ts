import { describe, expect, it } from "vitest";
import type { MediaItem } from "../../app/types";
import {
  detailLabel,
  isImage,
  markdownFor,
  sizeLabel,
  uploadable,
} from "../../app/utils/media";

const item = (change: Partial<MediaItem> = {}): MediaItem => ({
  id: 1,
  name: "0123456789abcdef0123456789abcdef.png",
  url: "https://blog.example.com/media/0123456789abcdef0123456789abcdef.png",
  originalName: "Holiday photo.png",
  mime: "image/png",
  size: 1536,
  width: 1200,
  height: 800,
  alt: "A beach",
  uploadedBy: "ada",
  createdAt: "2026-10-26T10:00:00Z",
  ...change,
});

describe("sizeLabel", () => {
  it("picks a readable unit", () => {
    expect(sizeLabel(512)).toBe("512 B");
    expect(sizeLabel(1536)).toBe("2 KB");
    expect(sizeLabel(3 * 1024 * 1024)).toBe("3.0 MB");
  });
});

describe("detailLabel", () => {
  it("shows dimensions when the server measured them", () => {
    expect(detailLabel(item())).toBe("1200 × 800 · 2 KB");
  });
  it("leaves them out for formats it did not decode", () => {
    expect(detailLabel(item({ width: undefined, height: undefined }))).toBe("2 KB");
  });
});

describe("markdownFor", () => {
  it("makes an image with its alt text", () => {
    expect(markdownFor(item())).toBe(`![A beach](${item().url})`);
  });
  it("drops brackets and line breaks that would end the alt text", () => {
    expect(markdownFor(item({ alt: "a [b]\nc" }))).toBe(`![a b c](${item().url})`);
  });
  it("links a PDF by its file name", () => {
    const pdf = item({ mime: "application/pdf", originalName: "Menu [v2].pdf" });
    expect(isImage(pdf)).toBe(false);
    expect(markdownFor(pdf)).toBe(`[Menu v2.pdf](${item().url})`);
  });
  it("falls back to the stored name for a PDF without an original name", () => {
    expect(markdownFor(item({ mime: "application/pdf", originalName: "" }))).toBe(
      `[${item().name}](${item().url})`,
    );
  });
});

describe("uploadable", () => {
  it("skips empty entries such as folders", () => {
    const files = [new File(["x"], "a.png"), new File([], "folder")];
    expect(uploadable(files).map((f) => f.name)).toEqual(["a.png"]);
    expect(uploadable(null)).toEqual([]);
  });
});
