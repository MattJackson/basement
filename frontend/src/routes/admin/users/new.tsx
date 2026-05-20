import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import { client } from "@/shared/api/client";
import { adminPage } from "@/shared/layout/adminPage";
import { toast } from "sonner";

type Invite = { token: string; expiresAt: string };

// Operator design rule: popups for 1-2 fields max. AddUser modal had
// 3 fields (username + password + invite-toggle) AND used a hand-rolled
// fixed-position div instead of the shadcn Dialog component (so it
// didn't even have proper modal a11y). Migrated to a route here.
export const Route = createFileRoute("/admin/users/new")({
  component: adminPage(NewUserPage),
});

function NewUserPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteOnly, setInviteOnly] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [invite, setInvite] = useState<Invite | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username.trim()) return;
    if (!inviteOnly && password.length < 8) {
      toast.error("Password must be at least 8 characters");
      return;
    }

    setSubmitting(true);
    try {
      // @ts-ignore — gen:api hasn't covered this endpoint yet
      const { data: result, response } = await client.POST("/admin/users", {
        body: { username: username.trim(), password, inviteOnly } as any,
      });
      if (!response.ok) {
        toast.error(`Failed to create user (status ${response.status})`);
        return;
      }
      if (result && (result as any).invite) {
        setInvite((result as any).invite as Invite);
      } else {
        toast.success("User created");
        navigate({ to: "/admin/users" });
      }
    } catch {
      toast.error("Failed to create user");
    } finally {
      setSubmitting(false);
    }
  };

  if (invite) {
    const link = `${window.location.origin}/auth/oidc/redeem/${invite.token}`;
    const copy = () => navigator.clipboard.writeText(link);
    return (
      <div className="space-y-6 max-w-2xl">
        <BackLink />
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Invite link created</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Send this URL to <span className="font-medium">{username}</span>. Expires {new Date(invite.expiresAt).toLocaleString()}.
          </p>
        </header>
        <section className="rounded-lg border bg-card p-6 space-y-3">
          <Label className="text-xs uppercase tracking-wider text-muted-foreground">Invite URL</Label>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded bg-muted px-3 py-2 text-sm font-mono break-all">{link}</code>
            <Button variant="outline" size="sm" onClick={copy}>Copy</Button>
          </div>
        </section>
        <Button onClick={() => navigate({ to: "/admin/users" })}>Done</Button>
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-2xl">
      <BackLink />

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">New user</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Create a user directly or generate an invite token for them to redeem.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => navigate({ to: "/admin/users" })} disabled={submitting}>Cancel</Button>
          <Button onClick={(e) => handleSubmit(e as any)} disabled={!username.trim() || submitting}>
            {submitting ? "Creating…" : inviteOnly ? "Generate invite" : "Create user"}
          </Button>
        </div>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4 rounded-lg border bg-card p-6">
        <div className="grid gap-2">
          <Label htmlFor="username">Username *</Label>
          <Input id="username" value={username} onChange={(e) => setUsername(e.target.value)} placeholder="alice" autoFocus />
        </div>

        <label className="flex items-center gap-2 cursor-pointer">
          <Checkbox checked={inviteOnly} onCheckedChange={(c) => setInviteOnly(c === true)} />
          <span className="text-sm font-medium">Generate invite token instead of setting a password</span>
        </label>

        {!inviteOnly && (
          <div className="grid gap-2">
            <Label htmlFor="password">Password * (8+ characters)</Label>
            <Input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••••" />
          </div>
        )}
      </form>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/admin/users" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
      ← Back to users
    </Link>
  );
}
