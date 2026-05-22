import { useState, useEffect } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { useElevationGuard } from "@/shared/auth/elevation";

// Sibling /admin/users/new exists, so /admin/users is auto-derived
// as a parent layout. Parent (users.tsx) is now Outlet-only; this
// file is the index that lists users.
// NOTE: do NOT wrap in adminPage() — parent layout owns the chrome.
export const Route = createFileRoute("/admin/users/")({
  component: UsersPage,
});

type User = {
  username: string;
  role: "admin" | "user";
  uiAdmin?: boolean;
};

function UsersPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [users, setUsers] = useState<User[]>([]);
  // v1.3.0a.3: user:delete is ELEVATED-min on the backend. The
  // openapi-fetch middleware pops the modal eagerly on a 403, but
  // doesn't auto-retry — wrapping the click handler in
  // runWithElevation closes that gap so one click after elevate
  // actually finishes the delete.
  const runWithElevation = useElevationGuard();

  useEffect(() => {
    fetchUsers();
  }, []);

  async function fetchUsers() {
    try {
      const { data } = await client.GET("/admin/users");
      if (data) {
        setUsers(data as User[]);
      }
    } catch (error) {
      toast.error("Failed to load users");
      navigate({ to: "/files" });
    } finally {
      setLoading(false);
    }
  }

  async function deleteUserOnce(username: string): Promise<void> {
    // openapi-fetch returns { data, error, response } — it never
    // throws, so we read response.status and synthesise the error
    // shape that runWithElevation pattern-matches on (code +
    // details.mode_required). Without this synthesis, a 403 just
    // returns {error} and runWithElevation never sees it.
    // @ts-ignore - API types not generated yet
    const result = await client.DELETE("/admin/users", { query: { id: username } });
    const { response, error } = result as { response: Response; error: unknown };
    if (response.ok) return;
    const body = error as
      | { error?: { code?: string; message?: string; details?: { mode_required?: "admin" | "elevated" | "user" } } }
      | null
      | undefined;
    const code = body?.error?.code ?? `HTTP_${response.status}`;
    const message = body?.error?.message ?? `deleteUser failed (HTTP ${response.status})`;
    const err = new Error(`${code}: ${message}`) as Error & {
      code?: string;
      status?: number;
      details?: { mode_required?: "admin" | "elevated" | "user" };
    };
    err.code = code;
    err.status = response.status;
    err.details = body?.error?.details;
    throw err;
  }

  async function handleDeleteUser(username: string) {
    if (!confirm(`Delete user ${username}?`)) return;

    try {
      await runWithElevation(() => deleteUserOnce(username));
      toast.success("User deleted");
      fetchUsers();
    } catch (error) {
      // ELEVATION_CANCELLED surfaces here as a plain Error; we
      // suppress that case (operator chose to back out) and only
      // surface real failures.
      if ((error as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error("Failed to delete user");
    }
  }

  if (loading) {
    return <div className="flex items-center justify-center min-h-[400px]">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">User Management</h1>
          <p className="text-muted-foreground mt-1">Create and manage users.</p>
        </div>
        <Button onClick={() => navigate({ to: "/admin/users/new" })}>+ New User</Button>
      </div>

      <table className="min-w-full divide-y divide-border">
        <thead className="bg-muted/50">
          <tr>
            <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground">Username</th>
            <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground">Role</th>
            <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground">UI Admin</th>
            <th className="px-6 py-3 text-right text-xs font-medium uppercase text-muted-foreground">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border bg-background">
          {users.map((user) => (
            <tr key={user.username}>
              <td className="px-6 py-4 whitespace-nowrap text-sm font-medium">{user.username}</td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-muted-foreground">{user.role}</td>
              <td className="px-6 py-4 whitespace-nowrap text-sm">
                {user.uiAdmin ? (
                  <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs bg-primary/10 text-primary">Yes</span>
                ) : (
                  <span className="text-muted-foreground">No</span>
                )}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <button onClick={() => handleDeleteUser(user.username)} className="text-destructive hover:text-destructive/80">
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

    </div>
  );
}
