import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { useUserRegion, useUserRegionBuckets } from "@/shared/api/queries";

// NOTE: do NOT wrap in userPage() — the parent layout
// (routes/files/regions/$rid.tsx) already wraps Outlet in userPage.
// Double-wrap → double-header.
export const Route = createFileRoute("/files/regions/$rid/")({
  component: RegionBuckets,
});

function RegionBuckets() {
  const { rid } = Route.useParams();
  const navigate = useNavigate();
  const { data: region } = useUserRegion(rid);
  const { data: buckets, isLoading, error } = useUserRegionBuckets(rid);
  const list = buckets ?? [];

  const title = region?.alias || region?.endpoint || "Region";

  return (
    <div className="space-y-6">
      <Link to="/files" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        &larr; Back to regions
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{title}</h1>
          {region && (
            <p className="text-sm text-muted-foreground mt-1">
              {region.endpoint}
              {region.region ? ` • ${region.region}` : ""}
            </p>
          )}
        </div>
        <Button variant="outline" onClick={() => navigate({ to: "/files" })}>
          Back
        </Button>
      </header>

      {error ? (
        <ErrorBanner message="Couldn't list buckets. The key may have lost access on the backend." />
      ) : isLoading ? (
        <BucketListSkeleton />
      ) : list.length === 0 ? (
        <EmptyState
          icon="database"
          title="No buckets visible"
          description="The key on this region can't see any buckets. Ask your cluster admin to grant it access."
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((b) => {
                const label = b.aliases?.[0] || b.id;
                return (
                  <TableRow
                    key={b.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => navigate({ to: "/files/regions/$rid/b/$bid", params: { rid, bid: b.id } })}
                  >
                    <TableCell className="font-medium">{label}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function BucketListSkeleton() {
  return (
    <div className="rounded-lg border bg-card overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Bucket</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...Array(5)].map((_, i) => (
            <TableRow key={i}>
              <TableCell>
                <Skeleton className="h-4 w-48" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
