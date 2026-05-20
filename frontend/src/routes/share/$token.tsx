import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";
import { useShareInfo, useShareAuth, useShareList } from "@/shared/api/queries";
import type { components } from "@/shared/api/types.gen";

export const Route = createFileRoute("/share/$token")({
  component: ShareRoute,
});

function ShareRoute() {
  const { token } = Route.useParams();

  const [password, setPassword] = useState("");
  const [prefix, setPrefix] = useState("");

  const infoQuery = useShareInfo(token);
  const authMutation = useShareAuth();
  const listQuery = useShareList(token ?? undefined, prefix);

  // Handle password submission
  const handlePasswordSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await authMutation.mutateAsync({ token, password });
    setPassword("");
    infoQuery.refetch();
  };

  // Navigate within share by setting prefix
  const navigateToPrefix = (newPrefix: string) => {
    setPrefix(newPrefix);
  };

  // Download handler - opens in new tab to bypass auth requirements
  const handleDownload = (key: string) => {
    window.open(`/api/v1/share/${encodeURIComponent(token)}/get?key=${encodeURIComponent(key)}`, "_blank");
  };

  // Loading state
  if (infoQuery.isLoading || infoQuery.isPending) {
    return <ShareShell><Skeleton className="h-32 w-full" /></ShareShell>;
  }

  // Error states from info query
  if (infoQuery.error) {
    const error = infoQuery.error as any;
    if (error.code === "SHARE_NOT_FOUND") {
      return <ShareShell><NotFoundState /></ShareShell>;
    }
    if (error.code === "SHARE_REVOKED" || error.status === 410) {
      return <ShareShell><RevokedOrExpiredState reason="revoked" /></ShareShell>;
    }
    if (error.code === "SHARE_EXPIRED") {
      return <ShareShell><RevokedOrExpiredState reason="expired" /></ShareShell>;
    }

    // Generic error - show banner but keep shell
    const errorMsg = typeof error === 'object' && error !== null ? 
      (error as { message?: string }).message || "Failed to load share" : 
      "Failed to load share";
    return (
      <ShareShell>
        <ErrorBanner message={errorMsg} />
      </ShareShell>
    );
  }

  const info = infoQuery.data;
  if (!info) {
    return <ShareShell><NotFoundState /></ShareShell>;
  }

  // Revoked or expired state
  if (info.revoked || info.expired) {
    const reason = info.revoked ? "revoked" : "expired";
    return <ShareShell><RevokedOrExpiredState reason={reason} /></ShareShell>;
  }

  // Download limit reached
  if (listQuery.error && (listQuery.error as Error & { status?: number }).status === 410) {
    return <ShareShell><LimitReachedState /></ShareShell>;
  }

  // Password required state
  if (info.requiresPassword) {
    return (
      <ShareShell>
        <Card className="w-full max-w-md mx-auto">
          <CardHeader>
            <h1 className="text-xl font-semibold text-center">{info.displayName || "Share Link"}</h1>
          </CardHeader>
          <CardContent>
            <form onSubmit={handlePasswordSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="password">Enter password</Label>
                <Input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  disabled={authMutation.isPending}
                />
              </div>
              <Button type="submit" className="w-full" disabled={authMutation.isPending}>
                {authMutation.isPending ? "Authenticating…" : "Unlock Share"}
              </Button>
            </form>
          </CardContent>
        </Card>
      </ShareShell>
    );
  }

  // Single object share - direct download
  if (!info.isDirectory) {
    return (
      <ShareShell>
        <SingleObjectState displayName={info.displayName ?? undefined} token={token} onDownload={() => handleDownload("")} />
      </ShareShell>
    );
  }

  // Prefix share - browser view
  return (
    <ShareShell>
      <PrefixBrowserState
        displayName={info.displayName ?? undefined}
        prefix={prefix}
        onNavigate={navigateToPrefix}
      />
    </ShareShell>
  );
}

// Minimal no-chrome shell for public shares
function ShareShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Header - minimal, no nav */}
      <header className="border-b px-4 py-3">
        <div className="flex items-center justify-between max-w-6xl mx-auto w-full">
          <span className="text-xs text-muted-foreground">Delivered by Basement UI</span>
        </div>
      </header>

      {/* Main content */}
      <main className="flex-1 px-4 py-8">
        <div className="max-w-6xl mx-auto w-full">{children}</div>
      </main>

      {/* Footer */}
      <footer className="border-t px-4 py-3">
        <p className="text-xs text-center text-muted-foreground">Basement UI</p>
      </footer>
    </div>
  );
}

// Not found state
function NotFoundState() {
  return (
    <Card className="w-full max-w-md mx-auto">
      <CardContent className="pt-6">
        <EmptyState
          icon="link"
          title="Share not found"
          description="This share link doesn&apos;t exist or has been removed."
        />
      </CardContent>
    </Card>
  );
}

// Revoked/expired state
function RevokedOrExpiredState({ reason }: { reason: "revoked" | "expired" }) {
  return (
    <Card className="w-full max-w-md mx-auto">
      <CardContent className="pt-6">
        <EmptyState
          icon="link"
          title={reason === "revoked" ? "Share revoked" : "Share expired"}
          description={
            reason === "revoked"
              ? "This share link has been revoked by the owner."
              : "This share link has expired and is no longer accessible."
          }
        />
      </CardContent>
    </Card>
  );
}

// Download limit reached state
function LimitReachedState() {
  return (
    <Card className="w-full max-w-md mx-auto">
      <CardContent className="pt-6">
        <EmptyState
          icon="link"
          title="Download limit reached"
          description="This share link has reached its download limit and is no longer accessible."
        />
      </CardContent>
    </Card>
  );
}

// Single object state - direct download button
function SingleObjectState({
  displayName,
  onDownload,
}: {
  displayName?: string;
  token?: string;
  onDownload: () => void;
}) {
  return (
    <Card className="w-full max-w-md mx-auto">
      <CardHeader>
        <h1 className="text-xl font-semibold text-center truncate">{displayName || "File"}</h1>
      </CardHeader>
      <CardContent className="pt-6">
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground text-center">
            This share is for a single file. Click the button below to download it.
          </p>
          <Button onClick={onDownload} className="w-full" size="lg">
            Download File
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

// Prefix browser - folder/file list with breadcrumb
function PrefixBrowserState({ 
  displayName, 
  prefix,
  onNavigate
}: { 
  displayName?: string;
  prefix: string;
  onNavigate: (p: string) => void;
}) {
  const token = Route.useParams().token;
  const listQuery = useShareList(token, prefix);

  // Build breadcrumb from prefix path
  const buildBreadcrumb = () => {
    if (!prefix || prefix === "") return [];
    
    const parts: Array<{ label: string; path: string }> = [];
    let current = "";
    
    // Split by / and build cumulative paths
    prefix.split("/").filter(Boolean).forEach((part, idx) => {
      current += (idx > 0 ? "/" : "") + part;
      parts.push({ label: part, path: current });
    });
    
    return parts;
  };

  const breadcrumb = buildBreadcrumb();

  // Handle folder click
  const handleFolderClick = (folderName: string) => {
    const newPrefix = prefix 
      ? `${prefix === "" ? "" : "/"}${folderName}`
      : folderName;
    onNavigate(newPrefix);
  };

  // Loading state for list
  if (listQuery.isLoading && prefix !== "") {
    return (
      <div className="space-y-4">
        <ShareTitle displayName={displayName} />
        {prefix !== "" && (
          <Breadcrumb items={breadcrumb} onNavigate={onNavigate} currentPath={prefix} />
        )}
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    );
  }

  // Empty state - no objects found
  if (!listQuery.isLoading && listQuery.data?.objects.length === 0) {
    return (
      <div className="space-y-4">
        <ShareTitle displayName={displayName} />
        {prefix !== "" && (
          <Breadcrumb items={breadcrumb} onNavigate={onNavigate} currentPath={prefix} />
        )}
        <Card>
          <CardContent className="pt-6">
            <EmptyState
              icon="folder"
              title="No files here"
              description="This folder is empty."
            />
          </CardContent>
        </Card>
      </div>
    );
  }

  // Normal view with table
  return (
    <div className="space-y-4">
      <ShareTitle displayName={displayName} />
      {prefix !== "" && (
        <Breadcrumb items={breadcrumb} onNavigate={onNavigate} currentPath={prefix} />
      )}
      
      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="text-right w-[140px]">Size</TableHead>
              <TableHead className="w-[180px]">Modified</TableHead>
              <TableHead className="text-right w-[100px]">Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {listQuery.data?.objects.map((obj) => (
              <ObjectRow
                key={obj.key}
                obj={obj}
                onDownload={() => window.open(`/api/v1/share/${encodeURIComponent(token)}/get?key=${encodeURIComponent(obj.key)}`, "_blank")}
              />
            ))}
            
            {listQuery.data?.prefixes?.map((folder, idx) => (
              <FolderRow 
                key={`folder-${idx}`}
                folderName={folder}
                fullPath={`${prefix === "" ? "" : prefix}/${folder}`}
                onNavigate={handleFolderClick}
              />
            ))}

            {listQuery.isPending && (
              <TableRow>
                <TableCell colSpan={4}>
                  <div className="py-2">
                    <Skeleton className="h-4 w-full" />
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>

        {/* Load more if truncated */}
        {listQuery.data?.isTruncated && (
          <div className="p-4 border-t">
            <Button 
              variant="outline" 
              onClick={() => listQuery.refetch()}
              disabled={listQuery.isPending}
              className="w-full"
            >
              Load more
            </Button>
          </div>
        )}
      </Card>

      {/* Share info footer */}
      <p className="text-xs text-muted-foreground">
        {listQuery.data?.objects.length ?? 0} files shown
      </p>
    </div>
  );
}

function ShareTitle({ displayName }: { displayName?: string }) {
  return (
    <div className="space-y-1">
      <h1 className="text-xl font-semibold text-center sm:text-left truncate">
        {displayName || "Shared Files"}
      </h1>
    </div>
  );
}

function Breadcrumb({ 
  items, 
  onNavigate, 
  currentPath 
}: { 
  items: { label: string; path: string }[];
  onNavigate: (p: string) => void;
  currentPath: string;
}) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => onNavigate("")}
        disabled={!currentPath}
        className="h-7 px-2"
      >
        Home
      </Button>
      
      {items.map((item, idx) => (
        <div key={idx} className="flex items-center gap-1">
          <span className="text-muted-foreground">/</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onNavigate(item.path)}
            disabled={item.path === currentPath}
            className={`h-7 px-2 ${item.path === currentPath ? "text-muted-foreground cursor-default hover:bg-transparent" : ""}`}
          >
            {item.label}
          </Button>
        </div>
      ))}

      {currentPath && (
        <BreadcrumbGoUp onNavigate={onNavigate} currentPath={currentPath} />
      )}
    </div>
  );
}

function ObjectRow({
  obj,
  onDownload,
}: {
  obj: components["schemas"]["ObjectInfo"];
  onDownload: () => void;
}) {
  void obj; // keep wide signature for future per-file metadata rows
  return (
    <TableRow className="group cursor-pointer hover:bg-muted/40">
      <TableCell className="font-medium">
        <span className="inline-flex items-center gap-2">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="h-4 w-4 text-muted-foreground"
          >
            <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z" />
            <polyline points="14 2 14 8 20 8" />
          </svg>
          {obj.key.split("/").pop() || obj.key}
        </span>
      </TableCell>
      <TableCell className="text-right tabular-nums text-muted-foreground">
        {humanizeBytes(obj.size)}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {obj.last_modified ? humanizeTime(obj.last_modified) : "—"}
      </TableCell>
      <TableCell className="text-right">
        <Button 
          variant="outline" 
          size="sm"
          onClick={onDownload}
          className="opacity-0 group-hover:opacity-100 transition-opacity"
        >
          Download
        </Button>
      </TableCell>
    </TableRow>
  );
}

function BreadcrumbGoUp({ onNavigate, currentPath }: { onNavigate: (p: string) => void; currentPath: string }) {
  const parts = currentPath.split("/").filter(Boolean);
  
  if (parts.length === 0) return null;
  
  parts.pop();
  
  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => onNavigate(parts.join("/"))}
      disabled={!currentPath}
      className="h-7 px-2"
    >
      ↑
    </Button>
  );
}

function FolderRow({ 
  folderName, 
  fullPath,
  onNavigate 
}: { 
  folderName: string;
  fullPath: string;
  onNavigate: (p: string) => void;
}) {
  return (
    <TableRow className="group cursor-pointer hover:bg-muted/40">
      <TableCell 
        className="font-medium"
        onClick={() => onNavigate(fullPath)}
      >
        <span className="inline-flex items-center gap-2">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="h-4 w-4 text-muted-foreground"
          >
            <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z" />
          </svg>
          {folderName}
        </span>
      </TableCell>
      <TableCell className="text-right text-muted-foreground">—</TableCell>
      <TableCell className="text-muted-foreground">—</TableCell>
      <TableCell className="text-right">
        <Button 
          variant="ghost" 
          size="sm"
          onClick={() => onNavigate(fullPath)}
          className="opacity-0 group-hover:opacity-100 transition-opacity"
        >
          Open
        </Button>
      </TableCell>
    </TableRow>
  );
}
