import { afterEach, describe, expect, it, vi } from "vitest";
import { request } from "../../app/utils/api";

afterEach(() => vi.unstubAllGlobals());
describe("authenticated requests", () => {
  it("gets a fresh CSRF token without browser persistence", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(
        Response.json({ authenticated: true, csrf: "session-bound" }),
      )
      .mockResolvedValueOnce(Response.json({ saved: true }));
    vi.stubGlobal("fetch", fetch);
    await request("/api/post", "PUT", { markdown: "body" });
    expect(fetch.mock.calls[1]?.[1]).toMatchObject({
      headers: { "X-CSRF-Token": "session-bound" },
      credentials: "same-origin",
    });
  });
  it("does not submit an expired-session mutation", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(Response.json({ authenticated: false }));
    vi.stubGlobal("fetch", fetch);
    await expect(request("/api/post", "PUT", {})).rejects.toThrow(
      "Your writing is still here",
    );
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("does not report a failed network request as saved", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof globalThis.fetch>().mockRejectedValue(new Error("offline")),
    );
    await expect(request("/api/posts")).rejects.toThrow("not saved");
  });
});
