// ADR-0001 cycle v0.9.0h — sticky migration banner.
//
// On every admin page, if the current user is a Host Admin and there's
// at least one Connection with orphaned legacy creds (access_key_id +
// secret_key still stored in config from the pre-BucketGrant model),
// render an amber warning bar inviting the operator to migrate.
//
// Auto-hides on click-through (the migration page) once the orphan
// list returns empty. Uses an inline SVG warning icon — no emoji
// literals (operator-banned; see project memory).
import { Link } from "@tanstack/react-router";
import { useOrphanCreds } from "@/shared/api/queries";
import { useUser } from "@/shared/auth/useUser";

export function MigrationBanner() {
  const { data: user } = useUser();
  const isHostAdmin = user?.uiAdmin === true;
  // Gate the query off uiAdmin so non-admins never make the call.
  // useOrphanCreds reads the same flag internally; this just avoids
  // the extra render churn.
  const { data, isLoading } = useOrphanCreds(isHostAdmin);

  if (!isHostAdmin || isLoading) return null;
  const count = data?.orphans?.length ?? 0;
  if (count === 0) return null;

  return (
    <div
      role="alert"
      aria-live="polite"
      className="sticky top-16 z-40 w-full border-b border-amber-500/40 bg-amber-500/10 text-amber-900 dark:text-amber-100"
    >
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3 text-sm">
        <div className="flex items-center gap-2 min-w-0">
          <WarningIcon />
          <span className="truncate">
            <span className="font-medium">Heads up</span> &mdash; {count}{" "}
            {count === 1 ? "cluster has" : "clusters have"} leftover S3
            credentials in config from the old model. These no longer work for
            object browsing.
          </span>
        </div>
        <Link
          to="/admin/migrations"
          className="inline-flex shrink-0 items-center gap-1 rounded-md border border-amber-600/40 bg-amber-100/60 dark:bg-amber-500/20 px-3 py-1 text-sm font-medium hover:bg-amber-100 dark:hover:bg-amber-500/30 transition-colors"
        >
          Migrate now <ArrowIcon />
        </Link>
      </div>
    </div>
  );
}

function WarningIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
      aria-hidden="true"
    >
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  );
}

function ArrowIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <line x1="5" y1="12" x2="19" y2="12" />
      <polyline points="12 5 19 12 12 19" />
    </svg>
  );
}
