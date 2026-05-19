import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from "react";
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useKeys } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/keys/")({
  component: adminPage(KeysScreen),
});

function KeysScreen() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const { data: keys, isLoading, error } = useKeys();

  const filteredKeys = keys?.filter((key) => {
    const nameMatch = key.name?.toLowerCase().includes(search.toLowerCase()) ?? false;
    return nameMatch;
  });

  const header = (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Access keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          S3 credentials with per-bucket permissions.
        </p>
      </div>
      <div className="flex items-center gap-2 w-full sm:w-auto">
        <Input
          placeholder="Search by name..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 sm:w-64"
        />
       <Button variant="outline" onClick={() => {}}>
          New
        </Button>
      </div>
    </header>
  );

  if (error) {
    return (
      <div className="space-y-6">
        {header}
        <ErrorBanner message="Couldn't connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {header}

      {isLoading ? (
        <div className="rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Access Key ID</TableHead>
                <TableHead>Buckets</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-48" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : filteredKeys?.length === 0 ? (
        <EmptyState
          icon="key"
          title={search ? "No keys match your search" : "No access keys created yet"}
          description={
            search
              ? "Try a different search term or create a new key."
              : "Create your first API access key to get started."
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Access Key ID</TableHead>
                <TableHead>Buckets</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredKeys?.map((key) => (
                <TableRow
                  key={key.id}
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => navigate({ to: "/admin/keys/$id", params: { id: key.id } })}
                >
                  <TableCell className="font-medium">{key.name ?? "-"}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm">
                        {key.id.slice(0, 12)}...{key.id.slice(-4)}
                      </span>
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-6 w-6 p-0"
                              onClick={(e) => {
                                e.stopPropagation();
                                navigator.clipboard.writeText(key.id);
                              }}
                            >
                              <svg
                                xmlns="http://www.w3.org/2000/svg"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                className="h-3 w-3"
                              >
                                <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
                                <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2v1" />
                              </svg>
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Copy key ID</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                  </TableCell>
                  <TableCell>
                    {/* Per-bucket permissions only live on GetKey detail,
                        not on ListKeys — surfaced on /admin/keys/$id (v0.2). */}
                    <span className="opacity-60">—</span>
                  </TableCell>
                  <TableCell>{humanizeTime(key.created)}</TableCell>
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
                         <DropdownMenuItem
                           onClick={(e) => {
                             e.stopPropagation();
                             navigate({ to: "/admin/keys/$id", params: { id: key.id } });
                           }}
                         >
                           View
                         </DropdownMenuItem>
                         {/* TODO(T2.38b) — delete key */}
                         <DropdownMenuItem variant="destructive" onClick={() => {}}>
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
