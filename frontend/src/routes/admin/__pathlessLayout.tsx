import type { ReactNode } from "react";
import { Outlet } from "@tanstack/react-router";
import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";
import { AppShell } from "@/shared/layout/AppShell";

interface AdminLayoutProps {}

function AdminLayout(_props: AdminLayoutProps): ReactNode {
  return (
    <ProtectedRoute>
      <AppShell>
        <Outlet />
      </AppShell>
    </ProtectedRoute>
  );
}

export const Route = {
  id: "__pathlessLayout",
  component: AdminLayout,
};
