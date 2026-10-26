import { describe, expect, it } from "vitest";
import type { AuditEntry } from "../../app/types";
import { auditActor, auditLabel, auditNeedsAttention } from "../../app/utils/audit";

const entry = (over: Partial<AuditEntry> = {}): AuditEntry => ({
  id: 1,
  at: "2026-10-26T09:00:00Z",
  actorId: 1,
  actor: "ada",
  action: "post.publish",
  target: "hello",
  remote: "203.0.113.5",
  ...over,
});

describe("auditLabel", () => {
  it("says what each action the server records means", () => {
    expect(auditLabel("login.failure")).toBe("Failed sign-in");
    expect(auditLabel("post.delete")).toBe("Post deleted");
    expect(auditLabel("deploy")).toBe("Rebuild requested");
    expect(auditLabel("role.change")).toBe("Role changed");
  });
  it("shows an action from a newer server as it is", () => {
    expect(auditLabel("post.teleport")).toBe("post.teleport");
  });
});

describe("auditActor", () => {
  it("names the account, and the token when there was one", () => {
    expect(auditActor(entry())).toBe("ada");
    expect(auditActor(entry({ tokenId: 4 }))).toBe("ada (token 4)");
  });
  it("shows the host and the scheduler, which have no account", () => {
    expect(auditActor(entry({ actorId: undefined, actor: "host" }))).toBe("host");
  });
});

describe("auditNeedsAttention", () => {
  it("flags failed sign-ins and recovery codes only", () => {
    expect(auditNeedsAttention(entry({ action: "login.failure" }))).toBe(true);
    expect(auditNeedsAttention(entry({ action: "2fa.recovery" }))).toBe(true);
    expect(auditNeedsAttention(entry({ action: "login.success" }))).toBe(false);
    expect(auditNeedsAttention(entry())).toBe(false);
  });
});
