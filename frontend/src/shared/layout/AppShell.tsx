import type { ReactNode } from "react";
import { useEffect } from "react";
import { Outlet, Link, useLocation, useNavigate } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { NewVersionBanner } from "@/shared/ui/NewVersionBanner";
import { PersonaPill } from "@/components/layout/PersonaPill";
import { ElevationExpiredBanner } from "@/components/auth/ElevationExpiredBanner";
import { useUser } from "@/shared/auth/useUser";
import { useAuthMode } from "@/shared/auth/mode";
import { useOnboardingState } from "@/shared/api/queries";
import { SkinInjector, OperatorFooter } from "@/shared/components/SkinInjector";

interface AppShellProps {
  children?: ReactNode;
}

// v1.11.0.17 mobile audit — admin nav links rendered at the bare text
// line-height (~20px) which is below the WCAG/iOS HIG 44×44 tap-target
// floor on phone viewports. Matching the v1.10.0.1 UserShell fix:
// inline-flex + items-center + min-h-[44px] pads the tap area to the
// full 44px box on touch devices while keeping the visual identical
// on desktop (the link text is what the eye registers).
const NAV_LINK =
  "text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center min-h-[44px]";
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
   // /login is the pre-auth page and renders bare (outside
// AppShell), but defensively skip it so a deep-link to it can't
// bounce.
if (location.pathname === "/login") return;
    // Defer while /auth/me is still loading — without this the redirect
    // fires on the first render before the cookie-derived mode lands
    // in the provider.
    if (userLoading) return;
    // Belt-and-braces: if the server payload says admin/elevated, the
    // hydrator hasn't run setMode yet for this render but the cookie
    // is already authoritative — don't bounce.
    if (user?.mode === "admin" || user?.mode === "elevated") return;
    // /login and /admin/login are pre-auth pages that render bare (outside
    // AppShell), so skip redirecting them to prevent deep-link bounces.
    if (location.pathname === "/login" || location.pathname === "/admin/login") return;
    if (mode === "user") {
      void navigate({ to: "/files", replace: true });
    }
  }, [onAdmin, location.pathname, mode, navigate, userLoading, user?.mode]);

  // v1.11.0a — first-run onboarding redirect. Auto-route to
  // /admin/first-run when the deploy is empty (0 clusters + 0
  // non-admin users) AND the operator hasn't dismissed the wizard
  // yet. We ONLY query for the state when the operator is in admin
  // mode on an /admin/* route — non-admin users get a 403 from the
  // state endpoint, and the query staying disabled avoids noise in
  // their browser console.
  //
  // We deliberately skip the redirect when the operator is ALREADY on
  // /admin/first-run (otherwise the route reload would loop) AND skip
  // every other deep-linked admin page (e.g. cluster detail) once the
  // wizard has been dismissed. The dismiss flag is the latch that
  // prevents auto-show; manual /admin/first-run navigation is always
  // allowed and the route renders fine even after dismiss.
  const isAdminUser = user?.uiAdmin === true;
  const inAdminMode = user?.mode === "admin" || user?.mode === "elevated" || mode !== "user";
  const onFirstRun = location.pathname === "/admin/first-run";
  const onLogin = location.pathname === "/login";
  const onboardingQueryEnabled =
    isAdminUser && inAdminMode && onAdmin && !onLogin && !userLoading;
  const { data: onboardingState } = useOnboardingState({ enabled: onboardingQueryEnabled });

  useEffect(() => {
    if (!onboardingQueryEnabled) return;
    if (onFirstRun) return;
    if (!onboardingState) return;
    if (onboardingState.completed) return;
    if (!onboardingState.needsOnboarding) return;
    void navigate({ to: "/admin/first-run", replace: true });
  }, [
    onboardingQueryEnabled,
    onFirstRun,
    onboardingState,
    navigate,
  ]);

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
          {/* v1.11.0.17 mobile audit — left cluster needs min-w-0 +
              flex-1 so the nav can shrink/scroll inside it instead of
              shoving the right cluster (PersonaPill/ThemeToggle/UserMenu)
              off-screen. Without this, every admin route's header
              overflowed the viewport by 200-260px on phones. */}
          <div className="flex items-center gap-3 sm:gap-6 min-w-0 flex-1">
            <Logo href={logoHref} />
            {/* v1.11.0.17 mobile audit — same scrollable-nav pattern as
                UserShell (v1.8.0e): on narrow viewports the nav
                scrolls horizontally rather than wrapping or pushing
                neighbours off the canvas. Scrollbar hidden for
                cleanliness; the partial-overflow cue at the edge
                tells the operator more nav exists. */}
            <nav
              className="flex items-center gap-5 overflow-x-auto whitespace-nowrap -mx-1 px-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
              aria-label="Primary"
            >
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
                  {/* v1.11.0.15: top-level "Keys" link removed. Keys
                      are inherently per-cluster (Garage admin model);
                      the canonical per-cluster keys list lives on
                      the cluster detail page at /admin/clusters/{cid}.
                      The orphan cross-cluster /admin/keys route was
                      removed in the same cycle. */}
                </>
              )}
            </nav>
          </div>
          <div className="flex items-center gap-1 sm:gap-2 flex-shrink-0">
            {/* ADR-0003 v1.2.0b: persona pill carries the live sudo */}
            {/* state (USER / ADMIN / ELEVATED) + a countdown and a */}
            {/* drop-privileges button. Sits before the user avatar */}
            {/* so the operator's eye lands on "what mode am I in?" */}
            {/* before "who am I logged in as?". */}
            {/* v1.13.0a (ADR-0008): the standalone ThemeToggle button */}
            {/* used to sit between PersonaPill and UserMenu. Moved */}
            {/* into the UserMenu as a Theme submenu so the page */}
            {/* chrome stays brand-clean for the pluggable-skins */}
            {/* surface. Per-user theme persists regardless of */}
            {/* skinPolicy. */}
            <PersonaPill />
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

      <SkinInjector />

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>

      <OperatorFooter />
    </div>
  );
}
