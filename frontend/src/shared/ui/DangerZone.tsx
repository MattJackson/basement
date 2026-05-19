import type { ReactNode } from "react";

export interface DangerZoneProps {
  /** Section title shown above the actions. Defaults to "Danger zone". */
  title?: string;
  /** Optional one-line helper explaining what's destructive about this. */
  description?: string;
  /** One or more destructive action buttons (Delete, Wipe, Disable, etc.). */
  children: ReactNode;
}

/**
 * DangerZone is the standard wrapper around irreversible / destructive
 * actions on detail pages. Used on bucket detail + key detail + (soon)
 * cluster detail. Visually consistent so the user learns: this region
 * is where the dangerous buttons live, and they always look like this.
 *
 * Style choices:
 *  - Top border separator so it visibly breaks from the rest of the page.
 *  - Small uppercase label so the user notices the regime change.
 *  - Optional helper text so an operator new to the area knows what
 *    "destructive" means here (e.g. "Deleting also removes all keys
 *    attached to this bucket.").
 */
export function DangerZone({ title = "Danger zone", description, children }: DangerZoneProps) {
  return (
    <section className="pt-6 mt-6 border-t" aria-label={title}>
      <h3 className="text-xs font-semibold uppercase tracking-wide text-destructive mb-1">
        {title}
      </h3>
      {description && (
        <p className="text-sm text-muted-foreground mb-3 max-w-prose">
          {description}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </section>
  );
}
