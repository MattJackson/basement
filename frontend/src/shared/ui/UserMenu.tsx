import { useState } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
import { useOrgCapabilities, useSetActiveSkin } from "@/shared/api/queries";
import { useTheme, type Theme } from "@/shared/theme/useTheme";
import { useSkinRegistry, useSkin } from "@/shared/hooks/useSkin";
import { useUser } from "@/shared/auth/useUser";
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
  // Controlled open so handlers (role switch → 423 elevation prompt)
  // can close the dropdown before the modal opens. Without this, the
  // dropdown sits on top of (or under) the elevation modal and steals
  // focus from the password input.
  const [menuOpen, setMenuOpen] = useState(false);
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

  return (
    <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
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
        {/* v1.13.35: dropped the username + role label header. The
            trigger button (avatar + username) already shows who's
            logged in, and the Role submenu trigger below shows the
            current activeRole — the old header duplicated the
            username AND showed the JWT role claim ("admin") even
            when the activeRole was "user", which confused operators
            in user mode. */}

 
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
        {/* v1.13.36: version footer removed from the dropdown. The
            same tag is already visible in the header under the
            "Basement" wordmark via <LogoVersion>, and the operator
            flagged the duplication. One source of truth. */}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
