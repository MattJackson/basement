import { createFileRoute } from "@tanstack/react-router";
import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";
import { useUser } from "@/shared/auth/useUser";

function AdminLanding() {
  const { data } = useUser();
  return (
    <ProtectedRoute>
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-center">
          <h1 className="text-4xl font-bold">basement</h1>
          <p className="mt-2 text-sm opacity-60">
            Welcome, {data?.username}. Dashboard coming in T2.27.
          </p>
        </div>
      </div>
    </ProtectedRoute>
  );
}

export const Route = createFileRoute("/admin/")({
  component: AdminLanding,
});
