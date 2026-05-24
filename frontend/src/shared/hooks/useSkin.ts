import { useMemo } from "react";
import { useActiveSkin, type Density, useSetActiveSkin as _useSetActiveSkin, useSkinRegistry as _useSkinRegistry } from "@/shared/api/queries";

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

  return {
    skin: computedSkin,
    isLoading,
    error,
    densityTokens,
    borderRadius: computedSkin?.borderRadius || defaultBorderRadius,
    setSelectedSkin,
  };
}
