import { useState, useEffect } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/users")({
  component: adminPage(UsersPage),
});

type User = {
  username: string;
  role: "admin" | "user";
  uiAdmin?: boolean;
};

type Invite = {
  token: string;
  expiresAt: string;
};

function UsersPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [users, setUsers] = useState<User[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [inviteResult, setInviteResult] = useState<Invite | null>(null);

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

  async function handleCreateUser(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const username = formData.get("username") as string;
    const password = formData.get("password") as string;
    const inviteOnly = formData.get("inviteOnly") === "on";

    try {
      // @ts-ignore - API types not generated yet
      const { data: result } = await client.POST("/admin/users", {
        body: { username, password, inviteOnly } as any,
      });

      if (result && "invite" in result) {
        setInviteResult(result.invite as Invite);
        setShowModal(true);
      } else {
        toast.success("User created successfully");
        fetchUsers();
      }
    } catch (error) {
      toast.error("Failed to create user");
    } finally {
      setShowModal(false);
    }
  }

  async function handleDeleteUser(username: string) {
    if (!confirm(`Delete user ${username}?`)) return;

    try {
      // @ts-ignore - API types not generated yet
      await client.DELETE("/admin/users", { query: { id: username } });
      toast.success("User deleted");
      fetchUsers();
    } catch (error) {
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
        <Button onClick={() => setShowModal(true)}>+ New User</Button>
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

      {showModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card p-6 rounded-lg w-full max-w-md space-y-4">
            <h2 className="text-xl font-bold">Create New User</h2>

            {inviteResult ? (
              <div className="space-y-3">
                <p className="text-sm text-muted-foreground">Invite link:</p>
                <code className="block p-3 bg-muted rounded-md text-sm break-all">
                  {window.location.origin}/auth/oidc/redeem/{inviteResult.token}
                </code>
                <Button onClick={() => setShowModal(false)}>Close</Button>
              </div>
            ) : (
              <form onSubmit={handleCreateUser} className="space-y-4">
                <input name="username" placeholder="Username" required className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
                <input name="password" type="password" placeholder="Password" required className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
                <label className="flex items-center gap-2">
                  <input name="inviteOnly" type="checkbox" className="h-4 w-4" />
                  Create invite token instead of direct account
                </label>
                <div className="flex justify-end gap-2">
                  <Button type="button" variant="outline" onClick={() => setShowModal(false)}>Cancel</Button>
                  <Button type="submit">Create</Button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
