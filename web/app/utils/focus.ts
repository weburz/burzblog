const focusable = (element: HTMLElement | null | undefined): element is HTMLElement => {
  if (!element || !element.isConnected || (element as HTMLButtonElement).disabled)
    return false;
  for (let node: HTMLElement | null = element; node; node = node.parentElement)
    if (node.hidden || node.style.display === "none") return false;
  return true;
};

// Busy controls become `disabled`, which drops keyboard focus to <body>. Run the
// operation, then hand focus back to the control that started it if nothing else
// claimed focus. When that control left the document or stayed disabled (Publish
// unmounts, Commit stays disabled with nothing selected), try the fallbacks in order.
export const withRetainedFocus = async <T>(
  operation: () => Promise<T>,
  fallbacks: () => (HTMLElement | null | undefined)[] = () => [],
): Promise<T> => {
  const active =
    typeof document === "undefined"
      ? null
      : (document.activeElement as HTMLElement | null);
  try {
    return await operation();
  } finally {
    if (active && active !== document.body && document.activeElement === document.body)
      [active, ...fallbacks()].find(focusable)?.focus({ preventScroll: true });
  }
};
