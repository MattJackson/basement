import type { ReactNode } from "react";
import { Outlet } from "@tanstack/react-router";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";

interface AppShellProps {
  children?: ReactNode;
}

/**
 * AppShell is the authed admin chrome: a single thin top bar (logo
 * left, controls right) and a content area. No sidebar — the UI is to
 * the files, not to menus. Secondary surfaces (cluster, keys, settings)
 * live behind the admin menu in the top-right.
 *
 * Header height is fixed at 56px (h-14). Content max-width is 1280px,
 * centered. The same outer `<div class="..content..">` shape is used
 * by every page so headers align across screens.
 */
export function AppShell({ children }: AppShellProps): ReactNode {
  return (
    <div className="min-h-screen bg-background flex flex-col">
      <header className="sticky top-0 z-30 h-14 w-full border-b bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60">
        <div className="h-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between gap-2">
          <Logo />
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
