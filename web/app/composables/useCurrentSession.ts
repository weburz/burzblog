import { request, rememberSession } from "../utils/api";
import type { Session } from "../types";

// Who is signed in, shared by every page. `expired` opens the sign-in dialog
// over the current page, so unsaved writing survives an ended session.
export const useCurrentSession = () => {
  const state = useState("session", () => ({
    ready: false,
    authenticated: false,
    username: "",
    expired: false,
  }));
  const apply = (session: Session) => {
    rememberSession(session);
    state.value.authenticated = session.authenticated;
    state.value.username = session.username ?? "";
  };
  const load = async () => {
    apply(await request<Session>("/api/session"));
    state.value.ready = true;
  };
  const signIn = async (username: string, password: string) => {
    apply(await request<Session>("/api/login", "POST", { username, password }));
    state.value.expired = false;
  };
  const signOut = async () => {
    await request("/api/logout", "POST", {}).catch(() => undefined);
    apply({ authenticated: false });
  };
  return { state, load, signIn, signOut };
};
