// Long settings lists show this many rows until the writer asks for all.
export const collapsedRows = 8;

export const visibleRows = <T>(rows: T[], showAll: boolean): T[] =>
  showAll ? rows : rows.slice(0, collapsedRows);

// "Show all 20", or "Show fewer" once expanded.
export const toggleLabel = (total: number, showAll: boolean): string =>
  showAll ? "Show fewer" : `Show all ${total}`;
