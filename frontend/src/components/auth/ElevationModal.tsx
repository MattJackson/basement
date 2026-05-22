// ADR-0003 / v1.2.0b + v1.2.0c — re-authentication prompt for
// sudo-style elevation.
//
// Opens when a request returns 403 + code=ELEVATION_REQUIRED (via
// ElevationProvider) or when a UI affordance explicitly asks the user
// to step up (e.g. UserMenu "Switch to admin view" in a later cycle).
//
// Local-password users (v1.2.0b path): one field, one submit. Per the
// project's "popups max 2 fields" memory this stays a confirmation-
// grade dialog — anything richer would become a route.
//
//   - POST /api/v1/auth/elevate {password, target_mode}
//   - 200 + RequiresOIDC=false: setAuthMode + onSuccess
//   - 200 + RequiresOIDC=true:  pivot to the OIDC flow (see below)
//   - 401: inline "Invalid password" error, keep modal open
//   - other: inline error from apiError shape, keep modal open
//
// OIDC-only users (v1.2.0c path): no password field. Single button —
// "Elevate via SSO" — that POSTs /auth/elevate/oidc/start and follows
// the returned redirect_url to the IdP. The callback handler mints
// the elevated cookie and 302s back to "/?elevated=<mode>"; the
// AuthModeHydrator picks the new mode up from the next /auth/me.
//
// The OIDC variant renders when the caller's /auth/me payload has
// oidcUser=true, OR when the dispatcher pivots a local-password
// submit to the OIDC flow (rare, but possible if an operator
// re-provisions a user as OIDC-only mid-session).

import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useSetAuthMode, type AuthMode } from "@/shared/auth/mode";
import { useUser } from "@/shared/auth/useUser";

export interface ElevationModalProps {
  open: boolean;
  /** Target mode to request. "admin" or "elevated" — never "user". */
  targetMode: Exclude<AuthMode, "user">;
  /** Optional one-line explanation rendered above the password field. */
  reason?: string;
  /** Called after a successful elevation. */
  onSuccess: () => void;
  /** Called on user-initiated dismissal (Cancel, Escape, backdrop). */
  onCancel: () => void;
}

interface ElevateResponseBody {
  mode?: AuthMode;
  mode_expires_at?: number; // unix SECONDS
  mode_ttl_seconds?: number;
  /** v1.2.0c: dispatcher pivots OIDC users to the OIDC challenge flow. */
  requires_oidc?: boolean;
  start_url?: string;
}

interface ElevateOIDCStartResponseBody {
  redirect_url: string;
}

export function ElevationModal({
  open,
  targetMode,
  reason,
  onSuccess,
  onCancel,
}: ElevationModalProps) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const setAuthMode = useSetAuthMode();
  const inputRef = useRef<HTMLInputElement | null>(null);

  // /auth/me's oidcUser flag drives the modal's variant: OIDC-only
  // users see a single "Elevate via SSO" button (no password field);
  // local-password users see the v1.2.0b password form. Until the
  // user query lands we default to the local-password form — it's the
  // strictly safer fallback (worst case: an OIDC user sees a password
  // field they can't satisfy, hits Submit, and the dispatcher pivots
  // them into the OIDC flow anyway via requires_oidc).
  const { data: user } = useUser();
  const isOIDCUser = user?.oidcUser === true;

  // Reset transient state whenever the modal opens/closes so a fresh
  // prompt doesn't show stale error copy from a previous attempt.
  // The setState here is deliberate — the open/close transition IS
  // the external trigger we're syncing against (parent toggles `open`
  // when its pending state changes).
  useEffect(() => {
    if (!open) {
      /* eslint-disable react-hooks/set-state-in-effect */
      setPassword("");
      setError(null);
      setBusy(false);
      /* eslint-enable react-hooks/set-state-in-effect */
      return;
    }
    // Focus the password input on open — same affordance the login
    // form gives, so keyboard users can type immediately. Skip for
    // the OIDC variant since there's no input to focus (the SSO
    // button is the only affordance and Dialog autofocuses the
    // first focusable element).
    if (isOIDCUser) return;
    const id = window.setTimeout(() => inputRef.current?.focus(), 50);
    return () => window.clearTimeout(id);
  }, [open, isOIDCUser]);

  const title =
    targetMode === "elevated"
      ? "Re-enter password to continue"
      : "Elevate to admin mode";

  const localDescription =
    targetMode === "elevated"
      ? "This destructive action requires fresh authentication. Re-enter your password to confirm."
      : "This action requires admin re-authentication. Your session stays as USER otherwise; re-enter your password to elevate.";

  const oidcDescription =
    targetMode === "elevated"
      ? "This destructive action requires a fresh SSO re-authentication. Continue to your identity provider to confirm."
      : "Admin re-authentication is handled by your identity provider. Continue to SSO to elevate; you'll return here automatically.";

  // pivotToOIDC kicks off the OIDC challenge flow: POST start, then
  // window.location.href = redirect_url so the IdP can re-auth the
  // user. The callback handler (server-side) mints the elevated
  // cookie and 302s back to "/?elevated=<mode>", which the
  // AuthModeHydrator notices on next /auth/me.
  const pivotToOIDC = async (startURL: string): Promise<void> => {
    const res = await fetch(startURL, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target_mode: targetMode }),
    });
    const body = (await res.json().catch(() => null)) as
      | ElevateOIDCStartResponseBody
      | { error?: { code?: string; message?: string } }
      | null;
    if (!res.ok || !body || !("redirect_url" in body) || !body.redirect_url) {
      if (res.status === 503) {
        setError(
          "OIDC is not configured on this server. Contact your administrator to enable elevation.",
        );
        return;
      }
      const errMsg =
        body && "error" in body && body.error?.message
          ? body.error.message
          : `OIDC elevation failed (HTTP ${res.status})`;
      setError(errMsg);
      return;
    }
    // Full-page nav to the IdP; the elevation completes server-side
    // via the callback handler when the browser comes back.
    window.location.href = body.redirect_url;
  };

  const handleOIDCSubmit = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await pivotToOIDC("/api/v1/auth/elevate/oidc/start");
    } catch {
      setError("Network error — please try again");
    } finally {
      setBusy(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (busy) return;
    if (!password) {
      setError("Password is required");
      return;
    }

    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/v1/auth/elevate", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password, target_mode: targetMode }),
      });
      const body = (await res.json().catch(() => null)) as
        | ElevateResponseBody
        | { error?: { code?: string; message?: string } }
        | null;

      if (res.status === 401) {
        setError("Invalid password");
        return;
      }

      // Dispatcher pivot: server says "this user is OIDC, go through
      // the OIDC start endpoint instead." We don't surface this as an
      // error — we just kick off the OIDC flow in place. The modal
      // stays open until window.location.href fires.
      if (
        res.ok &&
        body &&
        "requires_oidc" in body &&
        body.requires_oidc &&
        typeof body.start_url === "string" &&
        body.start_url.length > 0
      ) {
        await pivotToOIDC(body.start_url);
        return;
      }

      if (!res.ok || !body || !("mode" in body) || !body.mode) {
        const errMsg =
          body && "error" in body && body.error?.message
            ? body.error.message
            : `Elevation failed (HTTP ${res.status})`;
        setError(errMsg);
        return;
      }

      // Success: push the new mode + expiry into the provider so the
      // persona pill flips immediately. Convert seconds → ms once
      // here so the rest of the FE never has to think about the wire
      // unit.
      const expiresMs =
        typeof body.mode_expires_at === "number" && body.mode_expires_at > 0
          ? body.mode_expires_at * 1000
          : 0;
      setAuthMode({ mode: body.mode, expiresAt: expiresMs });
      onSuccess();
    } catch {
      setError("Network error — please try again");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? null : onCancel())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {isOIDCUser ? oidcDescription : localDescription}
          </DialogDescription>
        </DialogHeader>

        {isOIDCUser ? (
          <div className="flex flex-col gap-4">
            {reason && (
              <p className="text-sm text-muted-foreground">{reason}</p>
            )}

            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={onCancel}
                disabled={busy}
              >
                Cancel
              </Button>
              <Button
                type="button"
                onClick={handleOIDCSubmit}
                disabled={busy}
                data-testid="elevation-oidc-submit"
              >
                {busy ? "Redirecting…" : "Elevate via SSO"}
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            {reason && (
              <p className="text-sm text-muted-foreground">{reason}</p>
            )}

            <div className="space-y-2">
              <Label htmlFor="elevation-password">Password</Label>
              <Input
                id="elevation-password"
                ref={inputRef}
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={busy}
                placeholder="••••••••"
                data-testid="elevation-password"
              />
            </div>

            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={onCancel}
                disabled={busy}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={busy}>
                {busy ? "Verifying…" : "Submit"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
