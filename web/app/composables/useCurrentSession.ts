import { computed } from "vue";
import { request, rememberSession } from "../utils/api";
import type { Role, SecondFactorReply, Session } from "../types";

// Who is signed in, shared by every page. `expired` opens the sign-in dialog
// over the current page, so unsaved writing survives an ended session.
export const useCurrentSession = () => {
  const state = useState("session", () => ({
    ready: false,
    authenticated: false,
    username: "",
    role: "author" as Role,
    expired: false,
  }));
  // Only for showing or hiding controls; the server decides what is allowed.
  const isAdmin = computed(
    () => state.value.authenticated && state.value.role === "admin",
  );
  const apply = (session: Session) => {
    rememberSession(session);
    state.value.authenticated = session.authenticated;
    state.value.username = session.username ?? "";
    state.value.role = session.role ?? "author";
  };
  const load = async () => {
    apply(await request<Session>("/api/session"));
    state.value.ready = true;
  };
  // Resolves to a challenge when the account also needs a two-factor code;
  // finish with completeSignIn. Otherwise the user is signed in.
  const signIn = async (username: string, password: string): Promise<string | null> => {
    const reply = await request<Session | SecondFactorReply>("/api/login", "POST", {
      username,
      password,
    });
    if ("second_factor" in reply) return reply.challenge;
    apply(reply);
    state.value.expired = false;
    return null;
  };
  const completeSignIn = async (challenge: string, code: string) => {
    apply(await request<Session>("/api/login/totp", "POST", { challenge, code }));
    state.value.expired = false;
  };
  // Trades the one-time setup token for the first account and signs it in.
  const createFirstAccount = async (
    token: string,
    username: string,
    password: string,
  ) => {
    apply(await request<Session>("/api/setup", "POST", { token, username, password }));
    state.value.expired = false;
  };
  const signOut = async () => {
    await request("/api/logout", "POST", {}).catch(() => undefined);
    apply({ authenticated: false });
  };
  return { state, isAdmin, load, signIn, completeSignIn, createFirstAccount, signOut };
};
