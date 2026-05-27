import { useEffect, useMemo } from "react";
import { useActiveSkin, type Density, useSetActiveSkin as _useSetActiveSkin, useSkinRegistry as _useSkinRegistry } from "@/shared/api/queries";
import { useTheme } from "@/shared/theme/useTheme";

export { _useSetActiveSkin as useSetActiveSkin };
export { _useSkinRegistry as useSkinRegistry };

/**
 * v1.13.0c: Unified skin consumption hook.
 *
 * Returns the active skin with fallback defaults when fields are missing.
 * Provides computed density tokens and border radius for CSS consumption.
 */
export function useSkin() {
  const setActiveSkin = _useSetActiveSkin();
  const { data: skin, isLoading, error } = useActiveSkin();

  // Fallback values per ADR-0008 spec
  const defaultTypography = {
    sans: 'ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol", "Noto Color Emoji"',
    mono: 'ui-monospace, SFMono-Regular, "SF Mono", "Menlo", "Monaco", "Inconsolata", "Fira Mono", monospace',
  };

  const defaultDensity: Density = "comfortable";
  const defaultBorderRadius = "0.5rem";

  const computedSkin = useMemo(() => {
    if (!skin) return null;

    return {
      name: skin.name,
      displayName: skin.displayName || "Custom Skin",
      version: skin.version || "1.0.0",
      productName: skin.productName || "Basement",
      typography: {
        sans: skin.typography?.sans || defaultTypography.sans,
        mono: skin.typography?.mono || defaultTypography.mono,
        fontUrl: skin.typography?.fontUrl,
      },
      borderRadius: skin.borderRadius || defaultBorderRadius,
      density: skin.density || defaultDensity,
      footer: skin.footer,
      loginHero: skin.loginHero,
    };
  }, [skin]);

  // Density token mapping per spec: compact/comfortable/spacious
  const densityTokens = useMemo(() => {
    switch (computedSkin?.density) {
      case "compact":
        return { padding: "4px", gap: "8px" };
      case "spacious":
        return { padding: "12px", gap: "16px" };
      case "comfortable":
      default:
        return { padding: "8px", gap: "12px" };
    }
  }, [computedSkin?.density]);

  const setSelectedSkin = async (skinName: string) => {
    await setActiveSkin.mutateAsync(skinName);
  };

  // v1.13.20: ACTUALLY apply the skin's palette to the document.
  // v1.13.0a wired the --basement-* CSS variables in index.css, and
  // v1.13.0c wired typography/density/borderRadius/footer/loginHero —
  // but the palette was NEVER injected. Result: changing skin updated
  // state + showed new font/spacing but left the COLORS stuck on
  // basement-default. Operator-caught.
  //
  // Resolution: write each palette token to documentElement as a CSS
  // custom property. Pick light vs dark variant based on active theme
  // (system follows prefers-color-scheme). Tokens are: primary, bg, fg,
  // muted, accent, destructive, warning, success, info — same set
  // declared in index.css under the --basement-* prefix.
  const { isDark } = useTheme();

  useEffect(() => {
    if (!skin?.palette) return;
    const variant = isDark ? skin.palette.dark : skin.palette.light;
    if (!variant) return;
    const root = document.documentElement;
    // Map backend palette field names → frontend CSS variable suffix
    // (Palette type in internal/skin/skin.go uses bg/fg short forms;
    // frontend index.css uses --color-background / --color-foreground.)
    const mapping: Array<[keyof typeof variant, string]> = [
      ["primary", "primary"],
      ["bg" as keyof typeof variant, "background"],
      ["fg" as keyof typeof variant, "foreground"],
      ["muted", "muted"],
      ["accent", "accent"],
      ["destructive", "destructive"],
      ["warning", "warning"],
      ["success", "success"],
      ["info", "info"],
    ];
    for (const [paletteKey, cssName] of mapping) {
      const value = (variant as Record<string, string | undefined>)[paletteKey as string];
      if (typeof value === "string" && value) {
        root.style.setProperty(`--color-${cssName}`, `hsl(${value})`);
      }
    }
    // Common derived tokens follow the base palette so cards / popovers /
    // borders pick up the new colors without each skin needing to specify
    // them explicitly. These are the same defaults pattern index.css uses.
    const bg = (variant as Record<string, string | undefined>).bg;
    const fg = (variant as Record<string, string | undefined>).fg;
    if (bg) {
      root.style.setProperty("--color-card", `hsl(${bg})`);
      root.style.setProperty("--color-popover", `hsl(${bg})`);
    }
    if (fg) {
      root.style.setProperty("--color-card-foreground", `hsl(${fg})`);
      root.style.setProperty("--color-popover-foreground", `hsl(${fg})`);
    }
    // Cleanup is intentionally omitted: the next skin change will
    // overwrite, and a reset would briefly flash basement-default
    // between transitions. On unmount we want to keep the active skin
    // applied so the next mount picks up where we left off.
  }, [skin?.palette, isDark]);

  return {
    skin: computedSkin,
    isLoading,
    error,
    densityTokens,
    borderRadius: computedSkin?.borderRadius || defaultBorderRadius,
    setSelectedSkin,
  };
}
