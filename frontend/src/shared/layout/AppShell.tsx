import type { ReactNode } from "react";
import { Outlet, Link } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import { NewVersionBanner } from "@/shared/ui/NewVersionBanner";
import { PersonaPill } from "@/components/layout/PersonaPill";
import { ElevationExpiredBanner } from "@/components/auth/ElevationExpiredBanner";
import { useUser } from "@/shared/auth/useUser";

interface AppShellProps {
  children?: ReactNode;
}

const NAV_LINK =
  "text-sm text-muted-foreground hover:text-foreground transition-colors";
const NAV_LINK_ACTIVE = "text-foreground font-medium";

export function AppShell({ children }: AppShellProps): ReactNode {
  const { data: user } = useUser();
  const isUIAdmin = user?.uiAdmin === true;

  return (
    <div className="min-h-screen bg-background flex flex-col">
      <header className="sticky top-0 z-30 h-16 w-full border-b bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60">
        <div className="h-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between gap-2">
          <div className="flex items-center gap-6">
            <Logo />
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

      {/* ADR-0003 v1.3.0a.4 amendment: when an admin session expires */}
      {/* in place we surface a banner instead of auto-redirecting, so */}
      {/* the operator can finish reading / saving / re-elevate without */}
      {/* losing context. The banner self-gates on /admin/* + mode==user */}
      {/* so it's a no-op on /files routes. */}
      <ElevationExpiredBanner />

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>
    </div>
  );
}
