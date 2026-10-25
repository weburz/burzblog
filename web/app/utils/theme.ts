export type ThemePreference = "system" | "light" | "dark";

export const themeKey = "burzblog:theme";

export const themeOptions: { value: ThemePreference; label: string; icon: string }[] = [
  { value: "system", label: "System", icon: "i-lucide-monitor" },
  { value: "light", label: "Light", icon: "i-lucide-sun" },
  { value: "dark", label: "Dark", icon: "i-lucide-moon" },
];

export const asThemePreference = (value: unknown): ThemePreference =>
  value === "light" || value === "dark" ? value : "system";

export const isDark = (preference: ThemePreference, systemDark: boolean): boolean =>
  preference === "dark" || (preference === "system" && systemDark);

// Runs in <head> before the first paint so a dark screen never flashes white.
// Kept tiny and dependency-free; the server hashes it into the CSP.
export const themeBootScript = `(()=>{try{const t=localStorage.getItem("${themeKey}");const d=t==="dark"||(t!=="light"&&matchMedia("(prefers-color-scheme: dark)").matches);document.documentElement.classList.toggle("dark",d)}catch{}})()`;
