import type { ReactNode } from "react";
import { Outlet, Link } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import { NewVersionBanner } from "@/shared/ui/NewVersionBanner";
import { MigrationBanner } from "@/shared/ui/MigrationBanner";
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
            <span
              className="hidden sm:inline-flex items-center rounded-full border border-border bg-muted/40 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground"
              title="You are in the admin section. User section is at /files."
              aria-label="Admin section"
            >
              Admin
            </span>
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
            <ThemeToggle />
            <UserMenu />
          </div>
        </div>
      </header>

      <NewVersionBanner />
      <MigrationBanner />

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>
    </div>
  );
}
