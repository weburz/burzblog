import type { AuditEntry } from "../types";

// What the audit log's action names say, in words for the panel.
const labels: Record<string, string> = {
  "login.success": "Signed in",
  "login.failure": "Failed sign-in",
  "account.create": "Account created",
  "role.change": "Role changed",
  "password.change": "Password changed",
  "2fa.enable": "Two-factor turned on",
  "2fa.disable": "Two-factor turned off",
  "2fa.recovery": "Recovery code used",
  "token.create": "Token created",
  "token.revoke": "Token revoked",
  "post.publish": "Post published",
  "post.unpublish": "Post unpublished",
  "post.archive": "Post archived",
  "post.restore": "Post restored",
  "post.schedule": "Post scheduled",
  "post.unschedule": "Schedule cancelled",
  "post.delete": "Post deleted",
  "redirect.delete": "Redirect removed",
  deploy: "Rebuild requested",
  "backup.download": "Backup downloaded",
};

// An action the panel does not know yet (a newer server) shows as it is.
export const auditLabel = (action: string): string => labels[action] ?? action;

// Failed sign-ins, and a recovery code standing in for the authenticator,
// are the entries worth a second look.
export const auditNeedsAttention = (entry: AuditEntry): boolean =>
  entry.action === "login.failure" || entry.action === "2fa.recovery";

// Who did it: the account, and the token when it was not a browser.
export const auditActor = (entry: AuditEntry): string =>
  entry.tokenId ? `${entry.actor} (token ${entry.tokenId})` : entry.actor;
