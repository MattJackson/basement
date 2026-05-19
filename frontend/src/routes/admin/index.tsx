import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useBuckets } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/")({
  component: adminPage(MyBuckets),
});

/**
 * MyBuckets is the new landing — the primary admin surface. Each row
 * is a satisfying primary object: bucket name as the lead, aliases as
 * secondary, then size · objects · created on the right.
 *
 * Backend-agnostic note: when N>1 backend connections exist we'll
 * suffix each row with `· backend·label`; today (N=1) we drop it so
 * the user sees just the bucket name. The `Bucket` schema already
 * carries identity — we just don't surface it yet.
 */
function MyBuckets() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const { data: buckets, isLoading, error } = useBuckets();

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
  };

  const filteredBuckets = buckets?.filter((bucket) => {
    if (!search) return true;
    const needle = search.toLowerCase();
    return (bucket.aliases ?? []).some((a: string) => a.toLowerCase().includes(needle));
  });

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader
          title="My Buckets"
          description="Storage buckets you own or have access to."
          actions={<Button variant="outline" onClick={handleRefresh}>Refresh</Button>}
        />
        <ErrorBanner message="Couldn't connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="My Buckets"
        description="Storage buckets you own or have access to."
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Input
              placeholder="Search buckets..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="flex-1 sm:w-64"
            />
            {/* TODO(v0.2) — create bucket */}
            <Button variant="outline" onClick={() => {}}>
              New
            </Button>
          </div>
        }
      />

      {isLoading ? (
        <BucketListSkeleton />
      ) : filteredBuckets?.length === 0 ? (
        <EmptyState
          icon="database"
          title={search ? "No buckets match your search" : "No buckets yet"}
          description={
            search
              ? "Try a different search term."
              : "Create your first bucket to start storing files."
          }
        />
      ) : (
        <ul className="rounded-lg border bg-card divide-y divide-border">
          {filteredBuckets?.map((bucket) => {
            const aliases = bucket.aliases ?? [];
            const [primary, ...rest] = aliases;
            const name = primary ?? bucket.id.slice(0, 8);

            return (
              <li
                key={bucket.id}
                className="flex flex-col sm:flex-row sm:items-center gap-3 sm:gap-4 px-4 sm:px-6 py-4 hover:bg-muted/40 transition-colors group has-focus-visible:bg-muted/50"
              >
                {/* TODO(v0.2) — link to /admin/buckets/$id */}
                <button
                  type="button"
                  onClick={() => {}}
                  className="flex-1 min-w-0 text-left focus-visible:outline-none"
                  aria-label={`Open bucket ${name}`}
                >
                  <div className="flex items-baseline gap-2 min-w-0">
                    <span className="font-medium text-base truncate">{name}</span>
                    {rest.length > 0 && (
                      <span className="text-xs text-muted-foreground truncate">
                        also: {rest.join(", ")}
                      </span>
                    )}
                  </div>
                  {bucket.created && (
                    <p className="text-xs text-muted-foreground mt-0.5">
                      Created {humanizeTime(bucket.created)}
                    </p>
                  )}
                </button>

                <div className="flex items-center gap-6 text-sm">
                  <div className="text-right">
                    {/* Bytes + object counts come from per-bucket info
                        endpoint; the bucket-list endpoint omits them
                        (a follow-up in v0.2 — see GetBucket stats). */}
                    <div className="font-medium tabular-nums text-muted-foreground">
                      —
                    </div>
                    <div className="text-xs text-muted-foreground tabular-nums">
                      —
                    </div>
                  </div>

                  <DropdownMenu>
                    <DropdownMenuTrigger
                      className="rounded-md p-1.5 hover:bg-muted opacity-60 sm:opacity-0 sm:group-hover:opacity-100 sm:focus:opacity-100 transition-opacity"
                      aria-label={`Actions for ${name}`}
                    >
                      <svg
                        xmlns="http://www.w3.org/2000/svg"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        className="h-4 w-4"
                        aria-hidden="true"
                      >
                        <circle cx="12" cy="12" r="1" />
                        <circle cx="19" cy="12" r="1" />
                        <circle cx="5" cy="12" r="1" />
                      </svg>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      {/* TODO(v0.2) — view bucket details */}
                      <DropdownMenuItem onClick={() => {}}>View</DropdownMenuItem>
                      {/* TODO(v0.2) — delete bucket */}
                      <DropdownMenuItem variant="destructive" onClick={() => {}}>
                        Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}

function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          {title}
        </h1>
        {description && (
          <p className="text-sm text-muted-foreground mt-1">{description}</p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </header>
  );
}

function BucketListSkeleton() {
  return (
    <ul className="rounded-lg border bg-card divide-y divide-border">
      {[...Array(5)].map((_, i) => (
        <li key={i} className="px-4 sm:px-6 py-4 flex items-center gap-4">
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-3 w-24" />
          </div>
          <div className="space-y-2 text-right">
            <Skeleton className="h-4 w-20 ml-auto" />
            <Skeleton className="h-3 w-16 ml-auto" />
          </div>
        </li>
      ))}
    </ul>
  );
}
