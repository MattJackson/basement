// ADR-0003 / v1.2.0b + v1.2.0c — bridge between /auth/me and the
// AuthModeProvider.
//
// Sits inside the provider tree and pushes the server-reported mode +
// expiry into local state whenever the /auth/me query resolves with a
// fresh payload. Three cases drive this:
//
//   1. First load — context starts at the conservative USER default;
//      we upgrade it once the cookie-derived mode arrives.
//   2. Cookie rotation outside the FE — e.g. an admin op the backend
//      handled with a fresh sessionTTL stamp on the existing cookie.
//      A subsequent /auth/me refetch surfaces it and we re-sync.
//   3. OIDC elevation callback return (v1.2.0c) — the callback handler
//      302s the browser to "/?elevated=<mode>"; on first render we
//      detect that param, invalidate /auth/me to pick up the fresh
//      cookie, fire a success toast, and strip the param so a refresh
//      doesn't replay it.
//
// We compare incoming values against current state to avoid an
// infinite re-render loop (setState with the same object would
// otherwise be idempotent thanks to the useMemo in the provider, but
// being explicit keeps the intent obvious).

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useUser } from "@/shared/auth/useUser";
import { useAuthMode, useSetAuthMode, type AuthMode } from "@/shared/auth/mode";

export function AuthModeHydrator() {
  const { data: user } = useUser();
  const current = useAuthMode();
  const setMode = useSetAuthMode();
  const queryClient = useQueryClient();

  // v1.2.0c: pick up the OIDC elevation callback's `?elevated=<mode>`
  // and `?elevation_error=<code>` query params. Both are stripped
  // from the URL after handling so a refresh doesn't replay them.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    const elevated = url.searchParams.get("elevated");
    const elevationError = url.searchParams.get("elevation_error");
    if (!elevated && !elevationError) return;

    if (elevated === "admin" || elevated === "elevated") {
      toast.success(
        elevated === "elevated"
          ? "Elevated mode active — destructive actions are unlocked for 5 minutes."
          : "Admin mode active — re-authentication confirmed.",
      );
      // The new cookie is already on us via the callback's Set-Cookie;
      // refetching /auth/me feeds the fresh mode + expiry into the
      // provider via the seed effect below.
      queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
    } else if (elevationError) {
      toast.error(`Elevation failed: ${elevationError}`);
    }

    url.searchParams.delete("elevated");
    url.searchParams.delete("elevation_error");
    window.history.replaceState(
      null,
      "",
      url.pathname + (url.search ? url.search : "") + url.hash,
    );
  }, [queryClient]);

  useEffect(() => {
    if (!user) return;

    // Server payload uses seconds; provider stores ms.
    const serverMode: AuthMode = (user.mode as AuthMode | undefined) ?? "user";
    const serverExpiresMs =
      typeof user.modeExpiresAt === "number" && user.modeExpiresAt > 0
        ? user.modeExpiresAt * 1000
        : 0;

    if (current.mode === serverMode && current.expiresAt === serverExpiresMs) {
      return;
    }

    setMode({ mode: serverMode, expiresAt: serverExpiresMs });
  }, [user, current.mode, current.expiresAt, setMode]);

  return null;
}
