import { useState, useEffect } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { useElevationGuard } from "@/shared/auth/elevation";
import {
  useInvites,
  useCreateInvite,
  useRevokeInvite,
  useRotateInvite,
  type InvitePublic,
} from "@/shared/api/queries";

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
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">User Management</h1>
          <p className="text-muted-foreground mt-1">Create and manage users.</p>
        </div>
        <Button onClick={() => navigate({ to: "/admin/users/new" })}>+ New User</Button>
      </div>

      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-border">
          <thead className="bg-muted/50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground">Username</th>
              <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground sm:hidden">Role</th>
              <th className="px-6 py-3 text-left text-xs font-medium uppercase text-muted-foreground sm:hidden">UI Admin</th>
              <th className="px-6 py-3 text-right text-xs font-medium uppercase text-muted-foreground sm:hidden">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border bg-background">
            {users.map((user) => (
              <tr key={user.username}>
                <td className="px-6 py-4 text-sm font-medium">{user.username}</td>
                <td className="px-6 py-4 text-sm text-muted-foreground sm:hidden">{user.role}</td>
                <td className="px-6 py-4 sm:hidden">
                  {user.uiAdmin ? (
                    <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs bg-primary/10 text-primary">Yes</span>
                  ) : (
                    <span className="text-muted-foreground">No</span>
                  )}
                </td>
                <td className="px-6 py-4 text-right sm:hidden">
                  <button onClick={() => handleDeleteUser(user.username)} className="text-destructive hover:text-destructive/80">
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <PendingInvitesSection />
    </div>
  );
}

// PendingInvitesSection — v1.3.0d. Renders the operator's outstanding
// invite tokens with create / revoke / rotate / copy-full-token
// affordances. The token plaintext is only ever shown once (right
// after create or rotate) — afterwards we display the last-4 chars
// only, matching what the server stores.
function PendingInvitesSection() {
  const { data: invites, refetch, isLoading } = useInvites();
  const createMut = useCreateInvite();
  const revokeMut = useRevokeInvite();
  const rotateMut = useRotateInvite();

  const [label, setLabel] = useState("");
  // freshTokens tracks the plaintext of invites created or rotated
  // in this browser session. Keyed by invite ID; the value is the
  // plaintext token. We never persist these — they vanish on reload.
  const [freshTokens, setFreshTokens] = useState<Record<string, string>>({});

  const handleCreate = async () => {
    try {
      const res = await createMut.mutateAsync({ label: label.trim() || undefined });
      setFreshTokens((s) => ({ ...s, [res.invite.id]: res.token }));
      setLabel("");
      void refetch();
      toast.success("Invite created — copy the token now, it won't be shown again");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create invite");
    }
  };

  const handleRevoke = async (id: string) => {
    if (!confirm("Revoke this invite? The token will stop working immediately.")) return;
    try {
      await revokeMut.mutateAsync(id);
      setFreshTokens((s) => {
        const next = { ...s };
        delete next[id];
        return next;
      });
      void refetch();
      toast.success("Invite revoked");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to revoke");
    }
  };

  const handleRotate = async (id: string) => {
    if (!confirm("Generate a new token? The current one will stop working.")) return;
    try {
      const res = await rotateMut.mutateAsync({ id });
      setFreshTokens((s) => ({ ...s, [res.invite.id]: res.token }));
      void refetch();
      toast.success("Token rotated — copy the new one now");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to rotate");
    }
  };

  const handleCopyFresh = async (id: string) => {
    const plain = freshTokens[id];
    if (!plain) return;
    const url = `${window.location.origin}/auth/oidc/redeem/${plain}`;
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Redemption URL copied");
    } catch {
      toast.error("Clipboard copy failed");
    }
  };

  return (
    <section className="space-y-3" data-testid="pending-invites-section">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Pending invites</h2>
          <p className="text-muted-foreground text-sm mt-1">
            Mint a token, copy the redemption URL once, hand it to the invitee.
            Tokens expire 30 days after creation.
          </p>
        </div>
      </div>

      <div className="flex flex-col sm:flex-row sm:items-end gap-2 max-w-2xl">
        <div className="flex-1 space-y-1">
          <label htmlFor="invite-label" className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Label (optional)
          </label>
          <input
            id="invite-label"
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="wife"
            data-testid="invite-label-input"
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
        <Button onClick={handleCreate} disabled={createMut.isPending} data-testid="invite-create">
          {createMut.isPending ? "Creating..." : "+ New invite"}
        </Button>
      </div>

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Loading invites...</div>
      ) : !invites || invites.length === 0 ? (
        <div className="rounded-md border border-dashed bg-muted/20 px-4 py-6 text-sm text-muted-foreground" data-testid="invites-empty">
          No pending invites. Create one above to onboard a new user.
        </div>
      ) : (
        <div className="overflow-x-auto" data-testid="invites-table-wrapper">
          <table className="min-w-full divide-y divide-border" data-testid="invites-table">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-2 text-left text-xs font-medium uppercase text-muted-foreground">Label</th>
                <th className="px-4 py-2 text-left text-xs font-medium uppercase text-muted-foreground sm:hidden">Token</th>
                <th className="px-4 py-2 text-left text-xs font-medium uppercase text-muted-foreground sm:hidden">Created</th>
                <th className="px-4 py-2 text-left text-xs font-medium uppercase text-muted-foreground sm:hidden">Expires</th>
                <th className="px-4 py-2 text-right text-xs font-medium uppercase text-muted-foreground sm:hidden">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border bg-background">
              {invites.map((inv: InvitePublic) => (
                <tr key={inv.id} data-testid={`invite-row-${inv.id}`}>
                  <td className="px-4 py-3 text-sm font-medium">{inv.label || "—"}</td>
                  <td className="px-4 py-3 text-sm font-mono sm:hidden">
                    …{inv.tokenLast4}
                    {freshTokens[inv.id] && (
                      <button
                        type="button"
                        onClick={() => handleCopyFresh(inv.id)}
                        className="ml-2 rounded border px-2 py-0.5 text-xs hover:bg-muted/40"
                        data-testid={`copy-full-${inv.id}`}
                      >
                        Copy full URL
                      </button>
                    )}
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground sm:hidden">
                    {new Date(inv.createdAt).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-sm sm:hidden">
                    {inv.expired ? (
                      <span className="text-destructive">expired</span>
                    ) : (
                      <span className="text-muted-foreground">{new Date(inv.expiresAt).toLocaleDateString()}</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right text-sm whitespace-nowrap sm:hidden">
                    <button
                      onClick={() => handleRotate(inv.id)}
                      className="text-primary hover:underline mr-3"
                      data-testid={`rotate-${inv.id}`}
                    >
                      Rotate
                    </button>
                    <button
                      onClick={() => handleRevoke(inv.id)}
                      className="text-destructive hover:underline"
                      data-testid={`revoke-${inv.id}`}
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
