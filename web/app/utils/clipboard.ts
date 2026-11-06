// Copies text and says whether it worked. The clipboard API is missing on plain
// http from a non-localhost host (a Pi on the LAN), so a hidden textarea and
// `execCommand("copy")` is tried next. When both fail the caller shows the text
// where it can be selected by hand.
export const copyText = async (text: string): Promise<boolean> => {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Missing or blocked: fall through to the older path.
  }
  const field = document.createElement("textarea");
  field.value = text;
  field.setAttribute("readonly", "");
  field.setAttribute("aria-hidden", "true");
  field.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
  const active = document.activeElement;
  document.body.appendChild(field);
  try {
    field.select();
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    field.remove();
    if (active instanceof HTMLElement) active.focus();
  }
};
