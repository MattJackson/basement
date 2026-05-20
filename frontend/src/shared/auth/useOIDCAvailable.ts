import { useQuery } from "@tanstack/react-query";

interface AuthMethodsResponse {
  password: boolean;
  oidc: {
    configured: boolean;
    issuerLabel?: string;
  };
}

/**
 * useOIDCAvailable returns true when the server has an OIDC provider
 * configured. Used by the login page to decide whether to render the
 * "Sign in with SSO" button.
 *
 * Calls the public /api/v1/auth/methods endpoint (no auth required).
 * Replaces the earlier 501-probe of /auth/oidc/start, which worked but
 * had the side effect of setting an OIDC state cookie on every page
 * load even when the user never intended to sign in via SSO.
 *
 * Result is cached for the session so the login page doesn't re-fetch
 * on every render.
 */
export function useOIDCAvailable() {
  return useQuery<boolean>({
    queryKey: ["auth", "oidc-available"],
    queryFn: async () => {
      const res = await fetch("/api/v1/auth/methods", {
        method: "GET",
        credentials: "include",
      });
      if (!res.ok) return false;
      const body = (await res.json()) as AuthMethodsResponse;
      return body.oidc?.configured === true;
    },
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
  });
}
