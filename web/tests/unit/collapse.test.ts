import { describe, expect, it } from "vitest";
import { collapsedRows, toggleLabel, visibleRows } from "../../app/utils/collapse";

const rows = (n: number) => Array.from({ length: n }, (_, i) => i);

describe("collapsed lists", () => {
  it("keeps short lists whole", () => {
    expect(visibleRows(rows(collapsedRows), false)).toHaveLength(collapsedRows);
  });

  it("cuts long lists until all are asked for", () => {
    expect(visibleRows(rows(20), false)).toHaveLength(collapsedRows);
    expect(visibleRows(rows(20), true)).toHaveLength(20);
  });

  it("labels the toggle with the total", () => {
    expect(toggleLabel(20, false)).toBe("Show all 20");
    expect(toggleLabel(20, true)).toBe("Show fewer");
  });
});
