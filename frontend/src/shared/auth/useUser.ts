import { useQuery } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import type { UserResponse as UserResponseType } from "@/shared/api/mutations";

type AuthMode = "user" | "admin" | "elevated";

type UserResponseData = {
  id?: string;
  username: string;
  role: "admin" | "user";
  uiAdmin: boolean;
  // ADR-0003 v1.2.0b: sudo-style mode + expiry pulled off /auth/me so
  // the FE can hydrate its mode provider without a second roundtrip.
  // Optional — older backends omit them and the FE falls back to
  // "user" / 0 (which matches a default post-login state anyway).
  mode?: AuthMode;
  modeExpiresAt?: number; // unix SECONDS on the wire
  // v1.13.18: active role selector — one active role at a time
  activeRole?: {
    kind: "user" | "cluster-admin" | "ui-admin";
    cluster?: string;
  };
  availableRoles?: Array<{
    kind: "user" | "cluster-admin" | "ui-admin";
    label: string;
    cluster?: string;
  }>;
  // ADR-0003 v1.2.0c: true when this account was provisioned via OIDC
  // (no local password). The elevation modal branches on this — OIDC-
  // only users get an "Elevate via SSO" button that kicks off
  // /auth/elevate/oidc/start instead of the password form.
  oidcUser?: boolean;
};

export function useUser() {
  const result = useQuery<UserResponseData | undefined>({
    queryKey: ["auth", "me"],
    queryFn: async () => {
      const { data, response } = await client.GET("/auth/me");

      if (response.status === 401) {
        return undefined;
      }
      if (!response.ok || !data) {
        throw new Error(`Failed to fetch user (status ${response.status})`);
      }
      // Cast from backend User type to frontend UserResponseData with uiAdmin field
      const userData = data as unknown as {
        username: string;
        role: "admin" | "user";
        uiAdmin?: boolean;
        mode?: AuthMode;
        modeExpiresAt?: number;
        activeRole?: { kind: string; cluster?: string };
        availableRoles?: Array<{ kind: string; label: string; cluster?: string }>;
        oidcUser?: boolean;
      };
      return {
        id: (data as { id?: string }).id,
        username: userData.username,
        role: userData.role,
        uiAdmin: userData.uiAdmin ?? false,
        mode: userData.mode,
        modeExpiresAt: userData.modeExpiresAt,
        activeRole: userData.activeRole,
        availableRoles: userData.availableRoles,
        oidcUser: userData.oidcUser ?? false,
      } as UserResponseType;
    },
    staleTime: 5 * 60 * 1000,
    retry: (failureCount, error) => {
      if (error instanceof Error && error.message.includes("status 401")) return false;
      return failureCount < 2;
    },
  });

  return {
    data: result.data,
    isLoading: result.isLoading,
    isError: result.isError,
  };
}
