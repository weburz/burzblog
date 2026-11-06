import { afterEach, describe, expect, it, vi } from "vitest";
import { copyText } from "../../app/utils/clipboard";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const execCommand = (result: boolean | Error) => {
  const fn = vi.fn<(command: string) => boolean>(() => {
    if (result instanceof Error) throw result;
    return result;
  });
  Object.defineProperty(document, "execCommand", { value: fn, configurable: true });
  return fn;
};

describe("copyText", () => {
  it("uses the clipboard API when it works", async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue();
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const fallback = execCommand(true);
    await expect(copyText("hello")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("hello");
    expect(fallback).not.toHaveBeenCalled();
  });

  it("falls back to execCommand when the clipboard API is missing", async () => {
    vi.stubGlobal("navigator", {});
    const fallback = execCommand(true);
    await expect(copyText("hello")).resolves.toBe(true);
    expect(fallback).toHaveBeenCalledWith("copy");
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("falls back when the clipboard API rejects", async () => {
    vi.stubGlobal("navigator", {
      clipboard: {
        writeText: vi.fn<() => Promise<void>>().mockRejectedValue(new Error("denied")),
      },
    });
    execCommand(true);
    await expect(copyText("hello")).resolves.toBe(true);
  });

  it("reports failure when both paths fail", async () => {
    vi.stubGlobal("navigator", {});
    execCommand(false);
    await expect(copyText("hello")).resolves.toBe(false);
    execCommand(new Error("blocked"));
    await expect(copyText("hello")).resolves.toBe(false);
    expect(document.querySelector("textarea")).toBeNull();
  });
});
