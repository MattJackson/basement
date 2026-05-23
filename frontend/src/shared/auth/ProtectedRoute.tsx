import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate, useLocation } from "@tanstack/react-router";
import { useUser } from "./useUser";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

interface ProtectedRouteProps {
  children: ReactNode;
}

/**
 * ProtectedRoute redirects to /login if the user isn't
 * authenticated. Carries the current pathname as `?next=` so the
 * user lands back where they started after login.
 *
 * Guards against the recursive-next bug:
 *   - never redirect when already on /login (would just stack)
 *   - uses pathname (path-only), not href (full origin URL), so the
 *     next value is short and ergonomic
 *   - rejects `next` values that don't start with /admin (no
 *     open-redirect to attacker-controlled URLs)
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { data, isLoading } = useUser();

  useEffect(() => {
    if (isLoading || data) return;
    // Already on the login route? Nothing to do — don't recurse.
    if (location.pathname === "/login") return;
    const next = location.pathname.startsWith("/admin") ? location.pathname : "/files";
    navigate({ to: "/login", search: { next } });
  }, [isLoading, data, navigate, location]);

  if (isLoading || !data) {
    return <LoadingSpinner />;
  }

  return <>{children}</>;
}
