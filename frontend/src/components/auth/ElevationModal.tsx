// ADR-0003 / v1.2.0b — re-authentication prompt for sudo-style elevation.
//
// Opens when a request returns 403 + code=ELEVATION_REQUIRED (via
// ElevationProvider) or when a UI affordance explicitly asks the user
// to step up (e.g. UserMenu "Switch to admin view" in a later cycle).
//
// One field, one submit. Per the project's "popups max 2 fields"
// memory: this stays a confirmation-grade dialog — anything richer
// would become a route.
//
// On submit:
//   - POST /api/v1/auth/elevate {password, target_mode}
//   - 200: setAuthMode({mode, expiresAt: ms}), fire onSuccess()
//   - 401: inline "Invalid password" error, keep modal open
//   - other: inline error from apiError shape, keep modal open

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
  mode: AuthMode;
  mode_expires_at: number; // unix SECONDS
  mode_ttl_seconds: number;
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
    // form gives, so keyboard users can type immediately.
    const id = window.setTimeout(() => inputRef.current?.focus(), 50);
    return () => window.clearTimeout(id);
  }, [open]);

  const title =
    targetMode === "elevated"
      ? "Re-enter password to continue"
      : "Elevate to admin mode";

  const description =
    targetMode === "elevated"
      ? "This destructive action requires fresh authentication. Re-enter your password to confirm."
      : "This action requires admin re-authentication. Your session stays as USER otherwise; re-enter your password to elevate.";

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
      if (!res.ok || !body || !("mode" in body)) {
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
        body.mode_expires_at > 0 ? body.mode_expires_at * 1000 : 0;
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
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

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
      </DialogContent>
    </Dialog>
  );
}
