import { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useUserShares, useRevokeUserShare } from "@/shared/api/queries";
import { Button } from "@/components/ui/button";
import { CopyIcon, TrashIcon, LinkIcon } from "lucide-react";

function SharesList() {
  const [copiedToken, setCopiedToken] = useState<string | null>(null);
  const navigate = useNavigate();
  const sharesQuery = useUserShares();
  const revokeShare = useRevokeUserShare();

  if (sharesQuery.isLoading) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  if (sharesQuery.isError) {
    return <div className="text-red-500">Error loading shares: {String(sharesQuery.error)}</div>;
  }

  const shares = sharesQuery.data || [];

  const copyLink = async (token: string) => {
    const url = `${window.location.origin}/share/${token}`;
    await navigator.clipboard.writeText(url);
    setCopiedToken(token);
    setTimeout(() => setCopiedToken(null), 2000);
  };

  const handleRevoke = (token: string) => {
    if (!confirm("This will revoke the share link. Recipients will no longer be able to access it.")) {
      return;
    }
    revokeShare.mutate(token);
  };

  const formatDate = (dateStr: string | null | undefined) => {
    if (!dateStr) return "Never";
    return new Date(dateStr).toLocaleDateString();
  };

  const formatDateTime = (dateStr: string) => {
    return new Date(dateStr).toLocaleString();
  };

  return (
    <div className="space-y-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Shares</h1>
          <p className="text-sm text-muted-foreground mt-1">Links you've created</p>
        </div>
        <Button onClick={() => navigate({ to: "/files/shares/new" })}>+ New share</Button>
      </header>

      {shares.length === 0 ? (
        <div className="rounded-lg border border-dashed p-8 text-center space-y-3">
          <LinkIcon className="mx-auto h-12 w-12 text-muted-foreground" />
          <h3 className="text-lg font-medium">No shares yet</h3>
          <p className="text-sm text-muted-foreground max-w-md mx-auto">
            Create share links to give others access to your buckets or specific objects.
          </p>
          <Button onClick={() => navigate({ to: "/files/shares/new" })}>+ Create your first share</Button>
        </div>
      ) : (
        <div className="rounded-lg border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left p-4 font-medium">Bucket</th>
                <th className="text-left p-4 font-medium">Path</th>
                <th className="text-left p-4 font-medium">Created</th>
                <th className="text-left p-4 font-medium">Expires</th>
                <th className="text-left p-4 font-medium">Downloads</th>
                <th className="text-right p-4 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {shares.map((share) => (
                <tr key={share.token} className="hover:bg-muted/30 transition-colors">
                  <td className="p-4">
                    <div className="font-medium">{share.bucketAlias || share.bucketId}</div>
                    <div className="text-xs text-muted-foreground">{share.connectionId}</div>
                  </td>
                  <td className="p-4 font-mono text-xs break-all max-w-[200px]">
                    {share.path}
                  </td>
                  <td className="p-4">
                    <div>{formatDateTime(share.createdAt)}</div>
                  </td>
                  <td className="p-4">
                    {(() => {
                      const expires = share.expiresAt;
                      return (
                        <div className={expires ? "text-emerald-600" : "text-muted-foreground"}>
                          {formatDate(expires)}
                        </div>
                      );
                    })()}
                  </td>
                  <td className="p-4">
                    <div>{share.downloadsUsed}</div>
                    {share.downloadLimit && (
                      <div className="text-xs text-muted-foreground">/ {share.downloadLimit}</div>
                    )}
                  </td>
                  <td className="p-4 space-x-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => copyLink(share.token)}
                    >
                      {copiedToken === share.token ? "Copied!" : (
                        <>
                          <CopyIcon className="h-4 w-4 mr-1" />
                          Copy
                        </>
                      )}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleRevoke(share.token)}
                      disabled={revokeShare.isPending}
                    >
                      <TrashIcon className="h-4 w-4 mr-1" />
                      Revoke
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {revokeShare.isError && (
        <div className="text-red-500 text-sm">
          Error revoking share: {String(revokeShare.error)}
        </div>
      )}
    </div>
  );
}

import { userPage } from "@/shared/layout/userPage";

export const Route = createFileRoute("/files/shares")({
  component: userPage(SharesList),
});

export default SharesList;
