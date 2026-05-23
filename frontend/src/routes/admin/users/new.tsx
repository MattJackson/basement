import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import { client } from "@/shared/api/client";
import { toast } from "sonner";

// NOTE: do NOT wrap in adminPage() — parent layout (users.tsx) owns chrome.

type Invite = { token: string; expiresAt: string };

// Operator design rule: popups for 1-2 fields max. AddUser modal had
// 3 fields (username + password + invite-toggle) AND used a hand-rolled
// fixed-position div instead of the shadcn Dialog component (so it
// didn't even have proper modal a11y). Migrated to a route here.
export const Route = createFileRoute("/admin/users/new")({
  component: NewUserPage,
});

function NewUserPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteOnly, setInviteOnly] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [invite, setInvite] = useState<Invite | null>(null);
  // v1.10.0.2 — per-field validation errors. Pre-fix the submit button
  // was simply `disabled` when the username was blank, so an operator
  // clicking it on a pristine form got no feedback at all (smoke
  // section [C] flagged this as "blank submit silently does nothing",
  // matching the same shape we fixed for /files/keys/new and
  // /files/federated-buckets/new in v1.10.0.1). Now we let the click
  // fire, record which required fields are still empty, render inline
  // role="alert" messages + flip aria-invalid, and keep the user on
  // the page until they fix things.
  const [fieldErrors, setFieldErrors] = useState<{
    username?: string;
    password?: string;
  }>({});

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // v1.10.0.2 — collect per-field errors first so we can render
    // inline cues next to each offending input. Username is always
    // required; password is required (8+ chars) only when invite-only
    // is off.
    const nextFieldErrors: { username?: string; password?: string } = {};
    if (!username.trim()) nextFieldErrors.username = "Required";
    if (!inviteOnly && password.length < 8) {
      nextFieldErrors.password = "Password must be at least 8 characters";
    }
    setFieldErrors(nextFieldErrors);
    if (Object.keys(nextFieldErrors).length > 0) return;

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
          {/* v1.10.0.2 — submit no longer disabled-on-blank; the click
              must fire so inline validation can surface. The only
              disabled state now is during an in-flight request. */}
          <Button onClick={(e) => handleSubmit(e as any)} disabled={submitting}>
            {submitting ? "Creating…" : inviteOnly ? "Generate invite" : "Create user"}
          </Button>
        </div>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4 rounded-lg border bg-card p-6">
        <div className="grid gap-2">
          <Label htmlFor="username">Username *</Label>
          <Input
            id="username"
            value={username}
            onChange={(e) => {
              setUsername(e.target.value);
              if (fieldErrors.username && e.target.value.trim()) {
                setFieldErrors((prev) => ({ ...prev, username: undefined }));
              }
            }}
            placeholder="alice"
            autoFocus
            aria-invalid={fieldErrors.username ? true : undefined}
            aria-describedby={fieldErrors.username ? "username-error" : undefined}
            className={fieldErrors.username ? "border-red-500" : undefined}
          />
          {fieldErrors.username && (
            <p
              id="username-error"
              role="alert"
              className="text-xs text-red-600 dark:text-red-400"
            >
              {fieldErrors.username}
            </p>
          )}
        </div>

        <label className="flex items-center gap-2 cursor-pointer">
          <Checkbox
            checked={inviteOnly}
            onCheckedChange={(c) => {
              const v = c === true;
              setInviteOnly(v);
              // Flipping to invite-only clears any stale password error
              // since the password field no longer renders.
              if (v && fieldErrors.password) {
                setFieldErrors((prev) => ({ ...prev, password: undefined }));
              }
            }}
          />
          <span className="text-sm font-medium">Generate invite token instead of setting a password</span>
        </label>

        {!inviteOnly && (
          <div className="grid gap-2">
            <Label htmlFor="password">Password * (8+ characters)</Label>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
                if (fieldErrors.password && e.target.value.length >= 8) {
                  setFieldErrors((prev) => ({ ...prev, password: undefined }));
                }
              }}
              placeholder="••••••••"
              aria-invalid={fieldErrors.password ? true : undefined}
              aria-describedby={fieldErrors.password ? "password-error" : undefined}
              className={fieldErrors.password ? "border-red-500" : undefined}
            />
            {fieldErrors.password && (
              <p
                id="password-error"
                role="alert"
                className="text-xs text-red-600 dark:text-red-400"
              >
                {fieldErrors.password}
              </p>
            )}
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

// v1.10.0.2 — default export of the inner component so unit tests can
// render it in isolation (mirrors the same shape used by
// /files/keys/new.tsx). The TanStack route still owns mounting.
export default NewUserPage;
