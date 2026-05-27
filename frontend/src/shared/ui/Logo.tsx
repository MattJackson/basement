interface LogoProps {
  /** Optional href; defaults to `/admin` (the new landing). */
  href?: string;
  /** Suppress the wordmark — icon-only (e.g. for very narrow viewports). */
  iconOnly?: boolean;
  className?: string;
}

/**
 * Logo is the basement lockup used in the top bar: the simplified
 * favicon mark + "Basement" wordmark. We use `/favicon.svg` (the
 * rounded chest mark) rather than `/icon.svg` — at header size the
 * extra detail in icon.svg muddies, while the simpler mark reads as
 * a confident logo chip in both light and dark themes.
 *
 * Sizing: the mark is 40px (h-10) — large enough to register as a
 * brand, not a sticker — paired with a text-xl semibold wordmark.
 * On narrow viewports (iconOnly), the mark stands alone.
 *
 * The wordmark uses the document's default sans (Geist / system sans)
 * with tight tracking and semibold weight — the same restraint Linear
 * / Vercel use.
 */
export function Logo({ href = "/", iconOnly = false, className = "" }: LogoProps) {
  return (
    // v1.10.0.1 — added `min-h-[44px] min-w-[44px]` so the lockup
    // hits the WCAG/iOS HIG 44×44 tap-target threshold on mobile.
    // The mark is 40px (h-10 w-10) which the smoke audit flagged as
    // 4px short; the min-* utilities expand the anchor's hit area
    // without resizing the SVG itself, so desktop visuals are
    // unchanged.
    <a
      href={href}
      className={`flex items-center gap-2.5 font-medium hover:opacity-80 transition-opacity min-h-[44px] min-w-[44px] ${className}`}
      aria-label="Basement — home"
      data-testid="logo"
    >
      <img
        src="/favicon.svg"
        alt=""
        aria-hidden="true"
        className="h-10 w-10 shrink-0"
      />
      {!iconOnly && (
        <div className="flex flex-col leading-tight">
          <span className="text-xl tracking-tight font-semibold">
            Basement
          </span>
          <LogoVersion />
        </div>
      )}
    </a>
  );
}

import { useVersion } from "@/shared/api/queries";

function LogoVersion() {
  const { data } = useVersion();
  if (!data?.version) return null;
  return (
    <span className="text-[10px] text-muted-foreground/50 tabular-nums leading-none">
      {data.version}
    </span>
  );
}

