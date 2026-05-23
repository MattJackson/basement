import { Sun, Moon, Monitor } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useTheme, type Theme } from "./useTheme";

const ICONS: Record<Theme, typeof Sun> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
};

const LABELS: Record<Theme, string> = {
  light: "Light mode",
  dark: "Dark mode",
  system: "System theme",
};

/**
 * ThemeToggle cycles through system → light → dark → system. Stores
 * the choice in a cookie. Tooltip-friendly aria-label describes the
 * NEXT state for screen readers.
 */
export function ThemeToggle() {
  const { theme, cycleTheme } = useTheme();
  const Icon = ICONS[theme];
  const next: Theme =
    theme === "system" ? "light" : theme === "light" ? "dark" : "system";

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={`Theme: ${LABELS[theme]}. Click to switch to ${LABELS[next]}.`}
      title={LABELS[theme]}
      onClick={cycleTheme}
      // v1.10.0.2 — the icon size (`size-8` → 32px) was below the
      // WCAG/iOS HIG 44×44 mobile tap-target threshold flagged by the
      // v1.10.0.1 smoke audit. Force the tap area to >=44px on touch
      // viewports and snap back to the visual 32px size on desktop
      // where the smoke audit did not flag this control.
      className="min-h-[44px] min-w-[44px] sm:min-h-0 sm:min-w-0"
    >
      <Icon className="size-4" />
    </Button>
  );
}
