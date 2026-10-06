import { afterEach, describe, expect, it } from "vitest";
import { withRetainedFocus } from "../../app/utils/focus";

afterEach(() => {
  document.body.innerHTML = "";
});

const attach = <T extends HTMLElement>(element: T): T => {
  document.body.append(element);
  return element;
};

// Browsers drop focus to <body> when the focused control becomes disabled. happy-dom
// does not, and its blur() ignores disabled elements, so blur first and then disable.
const disableFocused = (button: HTMLButtonElement) => {
  button.blur();
  button.disabled = true;
  expect(document.activeElement).toBe(document.body);
};

describe("withRetainedFocus", () => {
  it("hands focus back to a control that was disabled while the operation ran", async () => {
    const button = attach(document.createElement("button"));
    button.focus();
    expect(document.activeElement).toBe(button);
    await withRetainedFocus(async () => {
      disableFocused(button);
      await Promise.resolve();
      button.disabled = false;
    });
    expect(document.activeElement).toBe(button);
  });

  it("leaves focus on the page when the control left the document", async () => {
    const button = attach(document.createElement("button"));
    button.focus();
    await withRetainedFocus(async () => {
      disableFocused(button);
      button.remove();
    });
    expect(document.activeElement).toBe(document.body);
  });

  it("returns the value of the operation", async () => {
    await expect(withRetainedFocus(async () => 42)).resolves.toBe(42);
  });

  it("rethrows the error of the operation", async () => {
    await expect(
      withRetainedFocus(async () => {
        throw new Error("x");
      }),
    ).rejects.toThrow("x");
  });

  it("hands focus back even when the operation throws", async () => {
    const button = attach(document.createElement("button"));
    button.focus();
    await expect(
      withRetainedFocus(async () => {
        disableFocused(button);
        button.disabled = false;
        throw new Error("x");
      }),
    ).rejects.toThrow("x");
    expect(document.activeElement).toBe(button);
  });

  it("leaves focus where it moved while the operation ran", async () => {
    const button = attach(document.createElement("button"));
    const field = attach(document.createElement("input"));
    button.focus();
    await withRetainedFocus(async () => {
      disableFocused(button);
      field.focus();
      button.disabled = false;
    });
    expect(document.activeElement).toBe(field);
  });

  it("falls back to the next focusable candidate when the control is gone", async () => {
    const button = attach(document.createElement("button"));
    const hidden = attach(document.createElement("textarea"));
    hidden.style.display = "none";
    const stillDisabled = attach(document.createElement("button"));
    const heading = attach(document.createElement("h1"));
    heading.tabIndex = -1;
    button.focus();
    await withRetainedFocus(
      async () => {
        disableFocused(button);
        button.remove();
        stillDisabled.disabled = true;
      },
      () => [hidden, stillDisabled, null, heading],
    );
    expect(document.activeElement).toBe(heading);
  });

  it("prefers the original control over the fallbacks", async () => {
    const button = attach(document.createElement("button"));
    const heading = attach(document.createElement("h1"));
    heading.tabIndex = -1;
    button.focus();
    await withRetainedFocus(
      async () => {
        disableFocused(button);
        button.disabled = false;
      },
      () => [heading],
    );
    expect(document.activeElement).toBe(button);
  });
});
