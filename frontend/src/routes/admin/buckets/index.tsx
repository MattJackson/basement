import { createFileRoute } from '@tanstack/react-router'
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";
import { useBuckets } from "@/shared/api/queries";

export const Route = createFileRoute("/admin/buckets/")({
  component: BucketsScreen,
});

function BucketsScreen() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const { data: buckets, isLoading, error } = useBuckets();

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
  };

  const filteredBuckets = buckets?.filter((bucket) =>
    bucket.global_aliases?.[0]?.toLowerCase().includes(search.toLowerCase()) ?? false
  );

  if (error) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Buckets</h1>
          <Button onClick={handleRefresh}>Refresh</Button>
        </div>
        <ErrorBanner message="Couldn't connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-3xl font-bold tracking-tight">Buckets</h1>
        <div className="flex items-center gap-2">
          <Input
            placeholder="Search by alias..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-64"
          />
          // TODO(v0.2) - Implement bucket creation
          <Button variant="outline" onClick={() => {}}>
            Create bucket
          </Button>
        </div>
      </div>

      {isLoading ? (
        <div className="rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Alias</TableHead>
                <TableHead>Objects</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>Quota</TableHead>
                <TableHead>Keys with access</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : filteredBuckets?.length === 0 ? (
        <EmptyState
          icon="database"
          title={search ? "No buckets match your search" : "No buckets created yet"}
          description={
            search
              ? "Try a different search term or create a new bucket."
              : "Create your first storage bucket to get started."
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Alias</TableHead>
                <TableHead>Objects</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>Quota</TableHead>
                <TableHead>Keys with access</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredBuckets?.map((bucket) => (
                <TableRow
                  key={bucket.id}
                  onClick={() => {}}
                  className="cursor-pointer hover:bg-muted/50"
                >
                  <TableCell className="font-medium">
                    {bucket.global_aliases?.[0] ?? "-"}
                  </TableCell>
                  <TableCell>{bucket.objects.toLocaleString()}</TableCell>
                  <TableCell>{humanizeBytes(bucket.bytes)}</TableCell>
                  <TableCell>-</TableCell>
                  <TableCell>
                    {bucket.keys?.length ?? 0}
                  </TableCell>
                  <TableCell>
                    {bucket.created_at ? humanizeTime(bucket.created_at) : "-"}
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger onClick={(e) => e.stopPropagation()}>
                        <Button variant="ghost" className="h-8 w-8 p-0">
                          <span className="sr-only">Open menu</span>
                          <svg
                            xmlns="http://www.w3.org/2000/svg"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="2"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            className="h-4 w-4"
                          >
                            <circle cx="12" cy="12" r="1" />
                            <circle cx="19" cy="12" r="1" />
                            <circle cx="5" cy="12" r="1" />
                          </svg>
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        // TODO(v0.2) - Implement view bucket details
                        <DropdownMenuItem onClick={() => {}}>View</DropdownMenuItem>
                        // TODO(v0.2) - Implement delete bucket
                        <DropdownMenuItem className="text-destructive" onClick={() => {}}>
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
