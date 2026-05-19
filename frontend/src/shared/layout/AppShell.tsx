import type { ReactNode } from "react";
import { Outlet, Link } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";

interface AppShellProps {
  children?: ReactNode;
}

const NAV_LINK =
  "text-sm text-muted-foreground hover:text-foreground transition-colors";
const NAV_LINK_ACTIVE = "text-foreground font-medium";

export function AppShell({ children }: AppShellProps): ReactNode {
  return (
    <div className="min-h-screen bg-background flex flex-col">
      <header className="sticky top-0 z-30 h-14 w-full border-b bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60">
        <div className="h-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between gap-2">
          <div className="flex items-center gap-6">
            <Logo />
            <nav className="flex items-center gap-5" aria-label="Primary">
              <Link
                to="/admin/cluster"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                Cluster
              </Link>
              <Link
                to="/admin/keys"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                Keys
              </Link>
            </nav>
          </div>
          <div className="flex items-center gap-1 sm:gap-2">
            <ThemeToggle />
            <UserMenu />
          </div>
        </div>
      </header>

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>
    </div>
  );
}
