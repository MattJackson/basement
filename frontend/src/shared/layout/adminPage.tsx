import type { ComponentType, ReactNode } from "react";
import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";
import { AppShell } from "./AppShell";

/**
 * adminPage wraps a route component with the admin chrome:
 * <ProtectedRoute><AppShell>{Page}</AppShell></ProtectedRoute>.
 *
 * Use this as the `component` value of every /admin/* route file
 * EXCEPT /login (which is pre-auth and renders bare).
 */
export function adminPage(Page: ComponentType): () => ReactNode {
  function AdminPageWrapper() {
    return (
      <ProtectedRoute>
        <AppShell>
          <Page />
        </AppShell>
      </ProtectedRoute>
    );
  }
  AdminPageWrapper.displayName = `adminPage(${Page.displayName ?? Page.name ?? "Component"})`;
  return AdminPageWrapper;
}
