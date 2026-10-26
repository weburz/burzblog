import { describe, expect, it } from "vitest";
import { caretScroll } from "../../app/utils/caret";

describe("caretScroll", () => {
  it("does nothing while the caret is comfortably in view", () => {
    expect(caretScroll(300, 330, 100, 700, 120)).toBe(0);
  });
  it("scrolls down when the caret nears the bottom edge", () => {
    expect(caretScroll(640, 670, 100, 700, 120)).toBe(90);
  });
  it("scrolls up when the caret nears the top edge", () => {
    expect(caretScroll(150, 180, 100, 700, 120)).toBe(-70);
  });
  it("prefers the bottom when the band is too small for both margins", () => {
    expect(caretScroll(200, 230, 100, 300, 120)).toBe(50);
  });
});
