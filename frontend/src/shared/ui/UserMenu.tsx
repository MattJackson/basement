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
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { client } from "@/shared/api/client";
import { useVersion, useOrgCapabilities, useSetActiveSkin } from "@/shared/api/queries";
import { promptElevationFromAnywhere } from "@/shared/auth/elevation";
import { useAuthMode } from "@/shared/auth/mode";
import { useTheme, type Theme } from "@/shared/theme/useTheme";
import { useSkinRegistry, useSkin } from "@/shared/hooks/useSkin";
import { useUser } from "@/shared/auth/useUser";
import { useSwitchActiveRole } from "@/shared/api/mutations";
import { SUPPORTED_LANGUAGES } from "@/shared/i18n";

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
  const { mode } = useAuthMode();
  // v1.13.0a (ADR-0008) — the light/dark/system toggle moved out of
  // the page chrome (formerly the standalone ThemeToggle button) and
  // into a Theme submenu here. Per-user always, regardless of the
  // org's skinPolicy — brand identity doesn't dictate whether a user
  // sees light or dark mode.
  const { theme, setTheme } = useTheme();
  const orgCaps = useOrgCapabilities();
  const skinRegistry = useSkinRegistry();
  const activeSkin = useSkin();
  const setActiveSkinMutation = useSetActiveSkin();
  const { t, i18n } = useTranslation("common");

  const username = user?.username ?? "—";
  const role = user?.role ?? "—";
  const activeRole = user?.activeRole;
  const availableRoles = user?.availableRoles ?? [];
  const initial = (user?.username ?? "?").charAt(0).toUpperCase();

  const switchActiveRoleMutation = useSwitchActiveRole();

  // handleSwitchToUser removed — role switching now handled via Role selector submenu
  
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

  // v1.13.18: Role selector handler
  const handleRoleChange = async (roleKey: string) => {
    if (!user?.activeRole || !availableRoles.length) return;

    // Find the selected role from available roles
    const selectedRole = availableRoles.find(r => r.kind === roleKey.split(":")[0] && (!r.cluster || r.cluster === roleKey.split(":")[1]));
    if (!selectedRole) return;

    try {
      await switchActiveRoleMutation.mutateAsync({
        kind: selectedRole.kind,
        cluster: selectedRole.cluster,
      });
      
      // Navigate to role-specific landing page after successful switch
      if (selectedRole.kind === "user") {
        await navigate({ to: "/files" });
      } else if (selectedRole.kind === "cluster-admin" && selectedRole.cluster) {
        await navigate({ to: `/admin/clusters/${selectedRole.cluster}` });
      } else if (selectedRole.kind === "ui-admin") {
        await navigate({ to: "/admin/system" });
      }
    } catch (error: any) {
      // v1.13.19: any 423 from PUT /auth/active-role means elevation is
      // required for that role. The apiError helper attaches .status
      // but NOT .details — the previous check `&& error?.details?.
      // requires_elevation` always failed second clause, falling through
      // to the bare-error toast. Simpler + correct: 423 → elevate.
      if (error?.status === 423) {
        try {
          const prompt = "Switching to this role requires admin re-authentication.";
          await promptElevationFromAnywhere("admin", prompt);
          
          // Retry the role switch after successful elevation
          await switchActiveRoleMutation.mutateAsync({
            kind: selectedRole.kind,
            cluster: selectedRole.cluster,
          });
          
          // Navigate to role-specific landing page
          if (selectedRole.kind === "user") {
            await navigate({ to: "/files" });
          } else if (selectedRole.kind === "cluster-admin" && selectedRole.cluster) {
            await navigate({ to: `/admin/clusters/${selectedRole.cluster}` });
          } else if (selectedRole.kind === "ui-admin") {
            await navigate({ to: "/admin/system" });
          }
        } catch (elevationError) {
          // Elevation cancelled or failed - show error but don't switch roles
          toast.error("Elevation required for this role and was cancelled");
        }
      } else {
        toast.error(error?.message || "Failed to switch role");
      }
    }
  };

  return (
    <DropdownMenu onOpenChange={(open) => {
      // Auto-navigate admin users to /admin/clusters when opening menu.
      if (open && mode === "admin") {
        void navigate({ to: "/admin/clusters" });
      }
    }}>
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
        {/* v1.13.18: Active role selector — replaces binary "Switch to admin/user view" */}
        {/* Dynamic, one active role at a time. User always available; cluster admins listed per grant; */}
        {/* UI Admin only if user.uiAdmin === true. Switching to admin roles triggers elevation prompt on 423 LOCKED. */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger data-testid="role-submenu-trigger">
            Role {activeRole && `(${availableRoles.find(r => r.kind === activeRole.kind && (activeRole.cluster ? r.cluster === activeRole.cluster : true))?.label || activeRole.kind})`}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuRadioGroup value={activeRole ? activeRole.kind + (activeRole.cluster ? ":" + activeRole.cluster : "") : "user"} onValueChange={handleRoleChange}>
              {availableRoles.map((r) => (
                <DropdownMenuRadioItem value={r.kind === "cluster-admin" && r.cluster ? `cluster-admin:${r.cluster}` : r.kind} key={r.kind + (r.cluster || "")} disabled={switchActiveRoleMutation.isPending}>
                  {r.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        {/* v1.13.31: admin nav split by active role per operator UX —
            "one owns clusters and access/modifications to clusters" (Cluster Admin)
            "one owns the ui and its settings/access/etc" (UI Admin).
            Previous v1.13.17 gate used legacy `mode === "admin" || "elevated"` which
            stayed true after switching activeRole back to "user" — auto-elevated UI
            admins saw the admin nav even after dropping. Now each item is gated to
            its OWN active role; nothing renders when activeRole.kind === "user". */}
        {activeRole?.kind === "cluster-admin" && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLinkItem href="/admin/clusters">
                {t("navigation.clusters")}
              </DropdownMenuLinkItem>
            </DropdownMenuGroup>
          </>
        )}
        {activeRole?.kind === "ui-admin" && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLinkItem href="/admin/policies">
                {t("navigation.policies")}
              </DropdownMenuLinkItem>
              <DropdownMenuLinkItem href="/admin/service-accounts">
                {t("navigation.serviceAccounts")}
              </DropdownMenuLinkItem>
              <DropdownMenuLinkItem href="/admin/audit">
                {t("navigation.audit")}
              </DropdownMenuLinkItem>
              <DropdownMenuLinkItem href="/admin/system">
                {t("navigation.system")}
              </DropdownMenuLinkItem>
            </DropdownMenuGroup>
          </>
        )}
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

        {/* v1.13.0c: Skin selector — only shown when org policy permits user choice */}
        {orgCaps.data?.userOverridableSkin && skinRegistry.data && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuSub>
              <DropdownMenuSubTrigger data-testid="skin-submenu-trigger">
                Skin
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                <DropdownMenuRadioGroup
                  value={activeSkin.skin?.name || ""}
                  onValueChange={(selectedName) => {
                    const allowed = orgCaps.data?.allowedUserSkins || [];
                    // Only allow selection if skin is in allowed set or list is empty (all skins available)
                    if (allowed.length === 0 || allowed.includes(selectedName)) {
                      setActiveSkinMutation.mutate(selectedName);
                    } else {
                      toast.error(`Skin "${selectedName}" is not available`);
                    }
                  }}
                >
                  {(orgCaps.data?.allowedUserSkins && orgCaps.data.allowedUserSkins.length > 0 
                    ? orgCaps.data.allowedUserSkins 
                    : skinRegistry.data.map(s => s.name)
                  ).map((name: string) => {
                    const skin = skinRegistry.data.find(s => s.name === name);
                    if (!skin) return null;
                    return (
                      <DropdownMenuRadioItem key={name} value={name}>
                        {skin.displayName || name}
                      </DropdownMenuRadioItem>
                    );
                  })}
                </DropdownMenuRadioGroup>
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            <DropdownMenuSeparator />
          </>
        )}

        {/* Language switcher */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger data-testid="language-submenu-trigger">
            Language
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuRadioGroup value={i18n.language} onValueChange={(lang) => i18n.changeLanguage(lang)}>
              {(SUPPORTED_LANGUAGES as readonly string[]).map((lang: string) => (
                <DropdownMenuRadioItem key={lang} value={lang}>
                  {lang === "en" ? "English" : lang === "es" ? "Español" : lang}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        <DropdownMenuItem onClick={handleLogout}>{t("auth.signOut")}</DropdownMenuItem>
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
