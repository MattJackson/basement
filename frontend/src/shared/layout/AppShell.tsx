import type { ReactNode } from "react";
import { useEffect } from "react";
import { Outlet, Link, useLocation, useNavigate } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import { NewVersionBanner } from "@/shared/ui/NewVersionBanner";
import { PersonaPill } from "@/components/layout/PersonaPill";
import { ElevationExpiredBanner } from "@/components/auth/ElevationExpiredBanner";
import { useUser } from "@/shared/auth/useUser";
import { useAuthMode } from "@/shared/auth/mode";

interface AppShellProps {
  children?: ReactNode;
}

const NAV_LINK =
  "text-sm text-muted-foreground hover:text-foreground transition-colors";
const NAV_LINK_ACTIVE = "text-foreground font-medium";

export function AppShell({ children }: AppShellProps): ReactNode {
  const { data: user, isLoading: userLoading } = useUser();
  const isUIAdmin = user?.uiAdmin === true;
  const { mode } = useAuthMode();
  const location = useLocation();
  const navigate = useNavigate();

  // v1.9.0e.2 — tight mode/view coupling. USER mode visiting /admin/*
  // gets a silent redirect to /files; no elevation prompt, no banner.
  // The operator must opt in to admin via the UserMenu (which elevates
  // up front and navigates to /admin/clusters on success).
  //
  // ADMIN mode visiting /files/* is allowed (admin can dip into the
  // user view), so the redirect only fires on the user→admin URL
  // mismatch.
  //
  // Also covers the falling-edge case: an admin session expiring
  // while on /admin/* drops the operator to /files, replacing the old
  // ElevationExpiredBanner re-elevate flow with a navigation that
  // matches the new mental model (drop = go user, elevate = go admin).
  //
  // v1.10.0e: hydration-race hardening (same shape as v1.7.0a.3/a.4).
  // The redirect now (1) waits for /auth/me to resolve before firing,
  // and (2) reads mode directly off the user payload as a fallback —
  // AuthModeHydrator's setMode runs in a SUBSEQUENT render so within
  // the first render where user data arrives, the provider's mode is
  // still the conservative USER default. Without these guards, every
  // full-page navigation to /admin/* would bounce to /files even when
  // the cookie reports admin.
  const onAdmin = location.pathname.startsWith("/admin");
  useEffect(() => {
    if (!onAdmin) return;
    // /admin/login is the pre-auth page and renders bare (outside
    // AppShell), but defensively skip it so a deep-link to it can't
    // bounce.
    if (location.pathname === "/admin/login") return;
    // Defer while /auth/me is still loading — without this the redirect
    // fires on the first render before the cookie-derived mode lands
    // in the provider.
    if (userLoading) return;
    // Belt-and-braces: if the server payload says admin/elevated, the
    // hydrator hasn't run setMode yet for this render but the cookie
    // is already authoritative — don't bounce.
    if (user?.mode === "admin" || user?.mode === "elevated") return;
    if (mode === "user") {
      void navigate({ to: "/files", replace: true });
    }
  }, [onAdmin, location.pathname, mode, navigate, userLoading, user?.mode]);

  // Logo target tracks mode under the tight coupling: USER → /files,
  // ADMIN → /admin. Without this the admin in /files clicks the logo
  // and lands on /admin (good) but the user shell's logo always points
  // at /files (good). Both shells delegate to <Logo href=...> and read
  // the same auth mode source of truth.
  const logoHref = mode === "user" ? "/files" : "/admin";

  return (
    <div className="min-h-screen bg-background flex flex-col">
      <header className="sticky top-0 z-30 h-16 w-full border-b bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60">
        <div className="h-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between gap-2">
          <div className="flex items-center gap-6">
            <Logo href={logoHref} />
            <nav className="flex items-center gap-5" aria-label="Primary">
              {/* 'Buckets' previously pointed at '/' which the role */}
              {/* gate redirects to '/admin/clusters' for UIAdmins — */}
              {/* so it landed on the same page as the Clusters nav */}
              {/* link. Now points directly at the aggregated buckets */}
              {/* view (lives at /admin/buckets, real route as of */}
              {/* v0.5.1 USER.ROUTING). */}
              <Link
                to="/admin/buckets"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                Buckets
              </Link>
              {isUIAdmin && (
                <>
                  {/* OBS.USAGE v0.9.0k — sits between Buckets and */}
                  {/* Clusters so the operator's natural left-to-right */}
                  {/* scan hits "what's there" (buckets) → "how much" */}
                  {/* (usage) → "where it lives" (clusters). */}
                  <Link
                    to="/admin/usage"
                    className={NAV_LINK}
                    activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
                  >
                    Usage
                  </Link>
                  <Link
                    to="/admin/clusters"
                    className={NAV_LINK}
                    activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
                  >
                    Clusters
                  </Link>
                  <Link
                    to="/admin/keys"
                    className={NAV_LINK}
                    activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
                  >
                    Keys
                  </Link>
                </>
              )}
            </nav>
          </div>
          <div className="flex items-center gap-1 sm:gap-2">
            {/* ADR-0003 v1.2.0b: persona pill carries the live sudo */}
            {/* state (USER / ADMIN / ELEVATED) + a countdown and a */}
            {/* drop-privileges button. Sits before the user avatar */}
            {/* so the operator's eye lands on "what mode am I in?" */}
            {/* before "who am I logged in as?". */}
            <PersonaPill />
            <ThemeToggle />
            <UserMenu />
          </div>
        </div>
      </header>

      <NewVersionBanner />

      {/* v1.9.0e.2: the ExpiredBanner still renders so an admin */}
      {/* session ending in /files/* (or anywhere the AppShell mounts) */}
      {/* surfaces the "re-elevate" affordance. On /admin/* the */}
      {/* redirect effect above fires first and the operator lands on */}
      {/* /files before the banner has a chance to matter. */}
      <ElevationExpiredBanner />

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>
    </div>
  );
}
