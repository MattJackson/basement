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
    >
      <Icon className="size-4" />
    </Button>
  );
}
