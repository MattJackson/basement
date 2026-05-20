import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ClusterBadge } from "@/components/ClusterBadge";
import type { components } from "@/shared/api/types.gen";
import { cn } from "@/lib/utils";

interface UserKeyPairCardProps {
  keyId: string;
  connectionId: string;
  bucketId?: string;
  bucketAliases?: string[] | null;
  keyName?: string;
  accessKeyId?: string | null;
  permissions?: components["schemas"]["BucketPermission"];
  created?: string | null;
}

export function UserKeyPairCard({
  keyId,
  connectionId,
  bucketAliases,
  keyName,
  accessKeyId,
  permissions,
  created,
}: UserKeyPairCardProps) {
  const displayLabel = bucketAliases && bucketAliases.length > 0 ? bucketAliases[0] : (keyName || "Unknown");

  const hasRead = permissions?.read ?? false;
  const hasWrite = permissions?.write ?? false;
  const isOwner = permissions?.owner ?? false;

  return (
    <Card data-testid="user-key-pair-card" className="group/card">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1 min-w-0">
            <CardDescription className="mb-1 truncate text-muted-foreground">
              Key for &quot;{displayLabel}&quot;
            </CardDescription>
            <div className="flex items-center gap-2 mb-1">
              <ClusterBadge connectionId={connectionId} size="sm" />
            </div>
          </div>
        </div>

        {keyName && (
          <CardTitle className="text-base truncate" title={keyName}>
            {keyName}
          </CardTitle>
        )}
      </CardHeader>

      <CardContent className="space-y-3">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground min-w-[70px]">Access Key ID</span>
          <code className="flex-1 rounded bg-muted px-2 py-1 font-mono text-xs truncate" data-testid="access-key-id-display">
            {accessKeyId ?? keyId}
          </code>
          <Button
            variant="ghost"
            size="sm"
            className="h-6 w-6 p-0 shrink-0"
            onClick={() => navigator.clipboard.writeText(accessKeyId ?? keyId)}
            title="Copy access key ID"
            data-testid="copy-key-button"
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
        </div>

        <div className="flex flex-wrap gap-1.5">
          {isOwner ? (
            <span data-testid="permission-owner-badge" className={cn(
              "inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-4xl border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-all",
              "bg-primary text-primary-foreground [a]:hover:bg-primary/80"
            )}>Owner</span>
          ) : hasWrite ? (
            <span data-testid="permission-write-badge" className={cn(
              "inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-4xl border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-all",
              "bg-secondary text-secondary-foreground [a]:hover:bg-secondary/80"
            )}>Write</span>
          ) : hasRead ? (
            <span data-testid="permission-read-badge" className={cn(
              "inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-4xl border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-all",
              "border-border text-foreground [a]:hover:bg-muted"
            )}>Read</span>
          ) : null}
        </div>

        {created && new Date(created).getFullYear() > 1970 && (
          <div className="text-xs text-muted-foreground pt-1">
            Created {new Date(created).toLocaleDateString()}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function UserKeyPairCardSkeleton() {
  return (
    <Card data-testid="user-key-pair-card-skeleton" className="group/card">
      <CardHeader>
        <div className="space-y-2">
          <Skeleton className="h-3 w-32" />
          <div className="flex items-center gap-2">
            <Skeleton className="h-4 w-16" />
          </div>
          <Skeleton className="h-4 w-24" />
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center gap-2">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="flex-1 h-3 w-48" />
          <Skeleton className="h-6 w-6 rounded" />
        </div>
        <div className="flex gap-1.5">
          <Skeleton className="h-5 w-16 rounded-full" />
        </div>
      </CardContent>
    </Card>
  );
}
