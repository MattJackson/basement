import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuLinkItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { useUser } from "@/shared/auth/useUser";
import { useVersion } from "@/shared/api/queries";
import { useElevationPrompt } from "@/shared/auth/elevation";
import { useAuthMode } from "@/shared/auth/mode";

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

  const username = user?.username ?? "—";
  const role = user?.role ?? "—";
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
    await navigate({ to: "/admin/login" });
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

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className="flex items-center gap-2 rounded-lg px-1.5 py-1 sm:px-3 sm:py-1.5 text-sm font-medium hover:bg-muted/50 transition-colors"
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
        {/* an admin manually visit the user view + back. Both links */}
        {/* render for now (pre-RBAC, everyone is admin); when RBAC */}
        {/* lands, hide the admin link for non-UIAdmins. */}
        {/* v1.3.0a.3: admin entry triggers elevation BEFORE navigating */}
        {/* so the first action under /admin doesn't 403; user entry */}
        {/* stays a plain link (no elevation needed for /files). */}
        <DropdownMenuGroup>
          <DropdownMenuItem
            onClick={handleSwitchToAdmin}
            data-testid="switch-to-admin"
          >
            Switch to admin view
          </DropdownMenuItem>
          <DropdownMenuLinkItem href="/files">
            Switch to user view
          </DropdownMenuLinkItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLinkItem href="/admin/clusters">
            Clusters
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/keys">
            Access keys
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/policies">
            Policies
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/service-accounts">
            Service accounts
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/audit">
            Audit log
          </DropdownMenuLinkItem>
          <DropdownMenuLinkItem href="/admin/system">
            System settings
          </DropdownMenuLinkItem>
        </DropdownMenuGroup>
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
