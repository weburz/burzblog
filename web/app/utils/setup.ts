// The setup link keeps its one-time token in the address fragment, which the
// browser never sends to a server or proxy: "#token=<token>".
export const setupTokenFromHash = (hash: string): string =>
  new URLSearchParams(hash.replace(/^#/, "")).get("token") ?? "";

export const minPassword = 12;

// What is wrong with the new password, as a sentence, or "" if it is fine.
// The server counts characters, not bytes or UTF-16 units, and so do we.
export const passwordProblem = (password: string, repeat: string): string => {
  if ([...password].length < minPassword)
    return `Use at least ${minPassword} characters.`;
  if (password !== repeat) return "The two passwords are different.";
  return "";
};
