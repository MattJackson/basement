// ADR-0003 / v1.7.0a.1 amendment — auto-elevate on /admin/* entry.
//
// Bug being fixed: navigating directly to /admin/clusters via the URL
// bar (or a deep link) renders the page in USER mode. PersonaPill says
// USER, but the URL says admin — every destructive click 403's with
// ELEVATION_REQUIRED until the operator manually clicks "Switch to
// admin". The UserMenu's "Switch to admin view" button already calls
// promptForElevation up front; URL-bar nav bypassed it.
//
// Fix: this guard sits inside AppShell (which wraps every /admin/*
// route). On mount + on pathname change, if mode === user AND the
// current path is under /admin/*, it triggers the same elevation
// modal the UserMenu uses. On cancel it routes the operator to /files
// with a toast ("Cancelled — staying in user view"). On success the
// page stays where it is and renders with mode = admin.
//
// Debounce: a useRef tracks which pathname we last fired for. Navigation
// within /admin/* (e.g. /admin/clusters → /admin/keys) does not re-
// prompt as long as the modal cycle hasn't completed yet — and even
// after it completes, we only re-prompt when the operator falls back to
// user mode (the auto-downgrade tick) AND lands on a fresh /admin path
// they haven't been prompted for.
//
// Render output: nothing. This is a side-effect-only component.

import { useEffect, useRef } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { useAuthMode } from "@/shared/auth/mode";
import { useElevationPrompt } from "@/shared/auth/elevation";

export function AdminEntryElevationGuard() {
  const { mode } = useAuthMode();
  const location = useLocation();
  const navigate = useNavigate();
  const promptForElevation = useElevationPrompt();

  // Tracks the last pathname we surfaced the prompt for. Reset to null
  // whenever the operator leaves /admin/* so a re-entry triggers a
  // fresh prompt. Also reset when mode flips to admin (rising edge)
  // so a future expiry+re-entry cycle prompts again.
  const lastPromptedPathRef = useRef<string | null>(null);
  // Pending guards against the StrictMode double-mount + against a
  // second pathname change racing with an in-flight modal.
  const pendingRef = useRef<boolean>(false);

  const onAdmin = location.pathname.startsWith("/admin");

  // Clear the latch when we leave /admin/* — next entry starts fresh.
  useEffect(() => {
    if (!onAdmin) {
      lastPromptedPathRef.current = null;
    }
  }, [onAdmin]);

  // Clear the latch when mode rises to admin — if the operator's
  // session later expires and they're still on /admin/*, we want the
  // banner+next-action 403 flow to handle it (the falling-edge case is
  // already covered by ElevationExpiredBanner). But if they navigate
  // OUT and back IN in user mode after expiry, the guard should fire
  // again — clearing the latch on the rising edge ensures the next
  // user-mode entry isn't suppressed by a stale value.
  useEffect(() => {
    if (mode === "admin" || mode === "elevated") {
      lastPromptedPathRef.current = null;
    }
  }, [mode]);

  useEffect(() => {
    if (!onAdmin) return;
    if (mode !== "user") return;
    if (pendingRef.current) return;
    // Already prompted for this exact pathname; respect the debounce.
    if (lastPromptedPathRef.current === location.pathname) return;

    lastPromptedPathRef.current = location.pathname;
    pendingRef.current = true;

    let cancelled = false;
    promptForElevation(
      "admin",
      "Admin pages require admin re-authentication.",
    )
      .then(() => {
        pendingRef.current = false;
        // Success: AuthModeHydrator flips mode to admin. Page stays.
      })
      .catch(() => {
        pendingRef.current = false;
        if (cancelled) return;
        toast.info("Cancelled — staying in user view");
        void navigate({ to: "/files" });
      });

    return () => {
      cancelled = true;
    };
  }, [onAdmin, mode, location.pathname, promptForElevation, navigate]);

  return null;
}
