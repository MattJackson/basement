import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuLinkItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { client } from "@/shared/api/client";
import { useUser } from "@/shared/auth/useUser";
import { useVersion } from "@/shared/api/queries";
import { useElevationPrompt } from "@/shared/auth/elevation";
import { useAuthMode, useSetAuthMode } from "@/shared/auth/mode";
import { useTheme, type Theme } from "@/shared/theme/useTheme";

/**
 * UserMenu is the admin menu in the top bar — avatar trigger,
 * dropdown contains Cluster, Settings (placeholder), Sign out, and a
 * tucked-away version tag at the bottom for ops.
 *
 * The trigger collapses to icon-only on narrow viewports (<sm) so it
 * functions as the mobile hamburger equivalent without a separate
 * affordance.
 */
export function UserMenu() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: user } = useUser();
  const { data: version } = useVersion();
  // v1.3.0a.3: "Switch to admin view" auto-elevates before navigating.
  // Operator mental model is "going into admin = entering admin mode";
  // decoupling URL from mode meant the first action under /admin always
  // 403'd and required a re-click after the elevate modal. We now pop
  // the modal up front and only navigate on successful elevation —
  // already-elevated users skip the prompt and go straight to /admin.
  const promptForElevation = useElevationPrompt();
  const { mode } = useAuthMode();
  const setAuthMode = useSetAuthMode();
  // v1.13.0a (ADR-0008) — the light/dark/system toggle moved out of
  // the page chrome (formerly the standalone ThemeToggle button) and
  // into a Theme submenu here. Per-user always, regardless of the
  // org's skinPolicy — brand identity doesn't dictate whether a user
  // sees light or dark mode.
  const { theme, setTheme } = useTheme();

  const username = user?.username ?? "—";
  const role = user?.role ?? "—";
  const uiAdmin = user?.uiAdmin ?? false;
  const initial = (user?.username ?? "?").charAt(0).toUpperCase();

  const handleLogout = async () => {
    try {
      await client.POST("/auth/logout");
    } catch (error) {
      console.error("logout request failed:", error);
    }
    // Clear cached user state so ProtectedRoute sees "logged out" and
    // redirects on the next render.
    queryClient.removeQueries({ queryKey: ["auth", "me"] });
    await navigate({ to: "/login" });
  };

  const handleSwitchToAdmin = async () => {
    if (mode === "admin" || mode === "elevated") {
      await navigate({ to: "/admin/clusters" });
      return;
    }
    try {
      await promptForElevation(
        "admin",
        "Switching to the admin console requires admin re-authentication.",
      );
      await navigate({ to: "/admin/clusters" });
    } catch {
      // ELEVATION_CANCELLED — user dismissed the modal. Stay where we
      // are; the UserMenu remains open visually only briefly because
      // Radix closes on item click anyway.
    }
  };

  // v1.9.0e.2: "Switch to user view" drops privileges before navigating
  // under the tight coupling. Mirror of handleDrop in PersonaPill —
  // POST /auth/logout-elevation, snap local mode to USER, invalidate
  // the cached /auth/me, then navigate. USER-mode clicks just navigate
  // (no privileges to drop).
  const handleSwitchToUser = async () => {
    if (mode === "user") {
      await navigate({ to: "/files" });
      return;
    }
    try {
      const res = await fetch("/api/v1/auth/logout-elevation", {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        toast.error(`Failed to drop privileges (HTTP ${res.status})`);
        return;
      }
      setAuthMode({ mode: "user", expiresAt: 0 });
      queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      await navigate({ to: "/files" });
    } catch {
      toast.error("Failed to drop privileges — network error");
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        // v1.10.0.2 — pre-fix the trigger rendered at ~40px tall on
        // mobile (the inner 32px avatar + 1px×2 padding), below the
        // WCAG/iOS HIG 44×44 tap-target threshold flagged in the
        // v1.10.0.1 smoke audit. min-h/min-w 44px on touch viewports
        // satisfies the threshold; sm: clears it on desktop where the
        // smoke didn't flag this control.
        className="flex items-center gap-2 rounded-lg px-1.5 py-1 sm:px-3 sm:py-1.5 text-sm font-medium hover:bg-muted/50 transition-colors min-h-[44px] min-w-[44px] sm:min-h-0 sm:min-w-0"
        aria-label="Open admin menu"
      >
        <span className="h-8 w-8 rounded-full bg-primary flex items-center justify-center text-primary-foreground">
          {initial}
        </span>
        <span className="hidden sm:inline">{username}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel>
            <div className="flex flex-col">
              <span className="font-medium">{username}</span>
              <span className="text-xs text-muted-foreground capitalize">{role}</span>
            </div>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {/* Persona switcher — admin ⇄ user. The role gate at `/` */}
        {/* routes UIAdmins to /admin and others to /files; this lets */}
        {/* an admin manually visit the user view + back. */}
        {/* v1.3.0a.3: admin entry triggers elevation BEFORE navigating */}
        {/* so the first action under /admin doesn't 403; user entry */}
        {/* stays a plain link (no elevation needed for /files). */}
        <DropdownMenuGroup>
          {role === "user" ? (
            <DropdownMenuItem onClick={handleSwitchToAdmin} data-testid="switch-to-admin">
              Switch to admin view
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onClick={handleSwitchToUser} data-testid="switch-to-user">
              Switch to user view
            </DropdownMenuItem>
          )}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLinkItem href="/admin/clusters">
            Clusters
          </DropdownMenuLinkItem>
          {/* v1.11.0.15: "Access keys" item removed — keys are
              per-cluster and live on the cluster detail page; the
              global /admin/keys route was retired in the same cycle. */}
          <DropdownMenuLinkItem href="/admin/policies">
            Policies
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/service-accounts">
            Service accounts
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/audit">
            Audit log
          </DropdownMenuLinkItem>
          {uiAdmin && (
            <DropdownMenuLinkItem href="/admin/system">
              System settings
            </DropdownMenuLinkItem>
          )}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {/* v1.13.0a (ADR-0008): Theme submenu — System / Light / Dark.
            Replaces the standalone ThemeToggle button that used to sit
            in the AppShell + UserShell headers. Radio-style; the
            current choice carries the check icon. */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger data-testid="theme-submenu-trigger">
            Theme
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(next) => setTheme(next as Theme)}
            >
              <DropdownMenuRadioItem
                value="system"
                data-testid="theme-system"
              >
                System
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem
                value="light"
                data-testid="theme-light"
              >
                Light
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem
                value="dark"
                data-testid="theme-dark"
              >
                Dark
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleLogout}>Sign out</DropdownMenuItem>
        {version?.version && (
          <>
            <DropdownMenuSeparator />
            <div
              className="px-1.5 py-1 text-[10px] font-mono opacity-40 select-text"
              title={
                version.commit
                  ? `commit ${version.commit.slice(0, 7)} · built ${version.builtAt}`
                  : undefined
              }
            >
              {version.version}
            </div>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
