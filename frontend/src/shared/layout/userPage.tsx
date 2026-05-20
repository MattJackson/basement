import type { ComponentType, ReactNode } from "react";
import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";
import { UserShell } from "./UserShell";

/**
 * userPage wraps a route component with the user chrome:
 * <ProtectedRoute><UserShell>{Page}</UserShell></ProtectedRoute>.
 *
 * Use this as the `component` value of every /files/* route file.
 */
export function userPage(Page: ComponentType): () => ReactNode {
  function UserPageWrapper() {
    return (
      <ProtectedRoute>
        <UserShell>
          <Page />
        </UserShell>
      </ProtectedRoute>
    );
  }
  UserPageWrapper.displayName = `userPage(${Page.displayName ?? Page.name ?? "Component"})`;
  return UserPageWrapper;
}
