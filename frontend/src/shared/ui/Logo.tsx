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
 * simpler glyph) rather than `/icon.svg` — at header size the extra
 * detail in icon.svg muddies, while the simpler mark reads as a
 * confident solid shape.
 *
 * The wordmark uses the document's default sans (Geist / system sans)
 * with tight tracking and semibold weight — the same restraint Linear
 * / Vercel use.
 */
export function Logo({ href = "/admin", iconOnly = false, className = "" }: LogoProps) {
  return (
    <a
      href={href}
      className={`flex items-center gap-2 font-medium hover:opacity-80 transition-opacity ${className}`}
      aria-label="Basement — home"
    >
      <img
        src="/favicon.svg"
        alt=""
        aria-hidden="true"
        className="h-6 w-6 shrink-0"
      />
      {!iconOnly && (
        <span className="text-base tracking-tight font-semibold">
          Basement
        </span>
      )}
    </a>
  );
}
