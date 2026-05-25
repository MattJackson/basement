import type { ReactNode } from "react";
import { Outlet, Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Logo } from "@/shared/ui/Logo";
import { UserMenu } from "@/shared/ui/UserMenu";
import { NewVersionBanner } from "@/shared/ui/NewVersionBanner";
import { PersonaPill } from "@/components/layout/PersonaPill";
import { useAuthMode } from "@/shared/auth/mode";

interface UserShellProps {
  children?: ReactNode;
}

// v1.10.0.1 — added `min-h-[44px] inline-flex items-center` to satisfy
// the WCAG/iOS HIG 44×44 tap-target threshold on mobile. Pre-fix the
// nav links rendered at the text line-height (~20px) which the smoke
// audit (section [E]) flagged as below the threshold. The inline-flex
// + items-center keeps the visual size identical on desktop (where the
// link text is what the eye registers) while padding the tap area to
// the full 44px box on touch devices.
const NAV_LINK =
  "text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center min-h-[44px]";
const NAV_LINK_ACTIVE = "text-foreground font-medium";

export function UserShell({ children }: UserShellProps): ReactNode {
  // v1.9.0e.2: logo target tracks mode. An ADMIN dipping into the user
  // view (allowed under the tight coupling) clicks the logo and lands
  // back on /admin, mirroring AppShell. USER mode stays on /files.
  const { mode } = useAuthMode();
  const logoHref = mode === "user" ? "/files" : "/admin";
  const { t } = useTranslation("common");

  return (
    <div className="min-h-screen bg-background flex flex-col">
      <header className="sticky top-0 z-30 h-16 w-full border-b bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60">
        <div className="h-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 flex items-center justify-between gap-2">
          <div className="flex items-center gap-3 sm:gap-6 min-w-0 flex-1">
            <Logo href={logoHref} />
            {/* v1.8.0e: nav scrolls horizontally on mobile (no
                hamburger). 7 top-level user routes wouldn't fit a
                380px viewport even at small font sizes, and a
                hamburger sheet adds friction for a daily-driver
                surface. overflow-x-auto with scrollbar-hidden gives
                operators a familiar swipe-the-tabs pattern (iOS
                Safari / Android Chrome both make this discoverable
                via the partial overflow cue at the edge). */}
            <nav
              className="flex items-center gap-5 overflow-x-auto whitespace-nowrap -mx-1 px-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
              aria-label="Primary"
              data-testid="user-nav"
            >
              <Link
                to="/files"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
                activeOptions={{ exact: true }}
              >
                {t("userNav.files")}
              </Link>
              <Link
                to="/files/keys"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                {t("userNav.keys")}
              </Link>
              <Link
                to="/files/shares"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                {t("userNav.shares")}
              </Link>
              <Link
                to="/files/backups"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                {t("userNav.backups")}
              </Link>
              <Link
                to="/files/federated-buckets"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                {t("userNav.federations")}
              </Link>
              <Link
                to="/files/webhooks"
                className={NAV_LINK}
                activeProps={{ className: `${NAV_LINK} ${NAV_LINK_ACTIVE}` }}
              >
                {t("userNav.webhooks")}
              </Link>
            </nav>
          </div>
          <div className="flex items-center gap-1 sm:gap-2 flex-shrink-0">
            {/* ADR-0003 v1.2.0b: persona pill — same one as AppShell. */}
            {/* Shown in the user shell too so a USER → ADMIN flip */}
            {/* (operator stepping up to do an admin op without */}
            {/* leaving /files) renders the countdown wherever they */}
            {/* happen to be. */}
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

      <main className="flex-1 w-full max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        {children ?? <Outlet />}
      </main>
    </div>
  );
}
