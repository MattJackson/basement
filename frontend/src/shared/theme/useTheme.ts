import { useCallback, useEffect, useState } from "react";

/**
 * Theme preference stored client-side. "system" follows the OS's
 * `prefers-color-scheme`; "light" / "dark" force the matching palette
 * regardless of OS setting.
 */
export type Theme = "system" | "light" | "dark";

const COOKIE_NAME = "basement_theme";
const COOKIE_MAX_AGE_DAYS = 365;

function readCookie(): Theme {
  if (typeof document === "undefined") return "system";
  const match = document.cookie
    .split(";")
    .map((c) => c.trim())
    .find((c) => c.startsWith(`${COOKIE_NAME}=`));
  if (!match) return "system";
  const value = match.slice(COOKIE_NAME.length + 1);
  if (value === "light" || value === "dark" || value === "system") return value;
  return "system";
}

function writeCookie(theme: Theme): void {
  const maxAge = COOKIE_MAX_AGE_DAYS * 24 * 60 * 60;
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie = `${COOKIE_NAME}=${theme}; Path=/; Max-Age=${maxAge}; SameSite=Lax${secure}`;
}

function systemPrefersDark(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function effectiveDark(theme: Theme): boolean {
  if (theme === "dark") return true;
  if (theme === "light") return false;
  return systemPrefersDark();
}

function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (effectiveDark(theme)) {
    root.classList.add("dark");
  } else {
    root.classList.remove("dark");
  }
  root.style.colorScheme = effectiveDark(theme) ? "dark" : "light";
}

/**
 * useTheme reads/writes the theme cookie and keeps <html class="dark">
 * in sync. Default is "system" (follows OS preference). Listens for
 * OS-preference changes when in "system" mode.
 */
export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => readCookie());

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    if (theme !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyTheme("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [theme]);

  const setTheme = useCallback((next: Theme) => {
    writeCookie(next);
    setThemeState(next);
  }, []);

  const cycleTheme = useCallback(() => {
    setThemeState((prev) => {
      const next: Theme =
        prev === "system" ? "light" : prev === "light" ? "dark" : "system";
      writeCookie(next);
      return next;
    });
  }, []);

  return { theme, setTheme, cycleTheme, isDark: effectiveDark(theme) };
}

/**
 * Apply the cookie-stored theme as early as possible — call before React
 * mounts to avoid a flash of the wrong palette.
 */
export function applyCookieThemeEarly(): void {
  if (typeof document === "undefined") return;
  applyTheme(readCookie());
}
