// ADR-0003 / v1.2.0b — bridge between /auth/me and the AuthModeProvider.
//
// Sits inside the provider tree and pushes the server-reported mode +
// expiry into local state whenever the /auth/me query resolves with a
// fresh payload. Two cases drive this:
//
//   1. First load — context starts at the conservative USER default;
//      we upgrade it once the cookie-derived mode arrives.
//   2. Cookie rotation outside the FE — e.g. an admin op the backend
//      handled with a fresh sessionTTL stamp on the existing cookie.
//      A subsequent /auth/me refetch surfaces it and we re-sync.
//
// We compare incoming values against current state to avoid an
// infinite re-render loop (setState with the same object would
// otherwise be idempotent thanks to the useMemo in the provider, but
// being explicit keeps the intent obvious).

import { useEffect } from "react";
import { useUser } from "@/shared/auth/useUser";
import { useAuthMode, useSetAuthMode, type AuthMode } from "@/shared/auth/mode";

export function AuthModeHydrator() {
  const { data: user } = useUser();
  const current = useAuthMode();
  const setMode = useSetAuthMode();

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
