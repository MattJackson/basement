import { useQuery } from "@tanstack/react-query";

/**
 * useOIDCAvailable returns true when the server has an OIDC provider
 * configured. Used by the login page to decide whether to render the
 * "Sign in with SSO" button.
 *
 * Why this hook instead of the /capabilities endpoint:
 *   /capabilities is mounted behind the JWT auth middleware
 *   (see internal/api/server.go), so it's unreachable from the login
 *   page. The Caps schema is also driver-scoped (versioning, presign,
 *   etc.) and adding OIDC config there would be a category error.
 *
 * Detection strategy: fetch /auth/oidc/start with redirect="manual". The
 * backend returns 501 OIDC_NOT_CONFIGURED when no provider is wired, and
 * either an opaque-redirect or a 302 when one is. Anything that isn't a
 * 501 means OIDC is available. The state cookie that the start endpoint
 * sets on the happy path is harmless here — it'll either be overwritten
 * on a real login attempt or expire after 5 minutes.
 *
 * Result is cached for the session so the login page doesn't re-probe on
 * every render.
 */
export function useOIDCAvailable() {
  return useQuery<boolean>({
    queryKey: ["auth", "oidc-available"],
    queryFn: async () => {
      const res = await fetch("/api/v1/auth/oidc/start", {
        method: "GET",
        credentials: "include",
        redirect: "manual",
      });
      // res.status === 0 happens with redirect: "manual" when the browser
      // gets a 3xx — treat that as "available". A real 501 is a clear
      // "not configured" signal.
      if (res.status === 501) return false;
      return true;
    },
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
  });
}
