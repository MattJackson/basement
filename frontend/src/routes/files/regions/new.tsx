import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useCreateUserRegion } from "@/shared/api/queries";

// /files/regions/new is the user-tier "I have S3 creds, here they
// are" flow per ADR-0002. Five fields (alias, endpoint, accessKey,
// secret, region) — more than the 2-field popup cap, so it stays a
// route per the popups-max-2-fields doctrine.
export const Route = createFileRoute("/files/regions/new")({
  component: userPage(NewRegionPage),
});

function NewRegionPage() {
  const navigate = useNavigate();
  const create = useCreateUserRegion();

  const [alias, setAlias] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [region, setRegion] = useState("");

  const isDisabled =
    !alias.trim() ||
    !endpoint.trim() ||
    !accessKeyId.trim() ||
    !secretKey.trim() ||
    create.isPending;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (isDisabled) return;
    create.mutate(
      {
        alias: alias.trim(),
        endpoint: endpoint.trim(),
        accessKeyId: accessKeyId.trim(),
        secretKey: secretKey.trim(),
        region: region.trim() || undefined,
      },
      { onSuccess: () => navigate({ to: "/files" }) },
    );
  };

  return (
    <div className="space-y-6 max-w-2xl">
      <Link to="/files" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        &larr; Back to regions
      </Link>

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Add a region</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Provide S3 credentials for an endpoint your cluster admin gave you.
          basement never shares the secret again — copy it now if you need
          to rotate later.
        </p>
      </header>

      {create.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {String((create.error as Error).message ?? "Failed to add region")}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-4 rounded-lg border bg-card p-6">
        <div className="grid gap-2">
          <Label htmlFor="alias">Alias *</Label>
          <Input
            id="alias"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            placeholder="home"
            autoFocus
          />
          <p className="text-xs text-muted-foreground">
            A short name for this keychain entry, e.g. &ldquo;home&rdquo; or &ldquo;work&rdquo;.
          </p>
        </div>

        <div className="grid gap-2">
          <Label htmlFor="endpoint">S3 endpoint *</Label>
          <Input
            id="endpoint"
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://s3.example.com"
          />
        </div>

        <div className="grid gap-2">
          <Label htmlFor="accessKeyId">Access key ID *</Label>
          <Input
            id="accessKeyId"
            value={accessKeyId}
            onChange={(e) => setAccessKeyId(e.target.value)}
            placeholder="GK..."
          />
        </div>

        <div className="grid gap-2">
          <Label htmlFor="secretKey">Secret key *</Label>
          <Input
            id="secretKey"
            type="password"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder="copy-once"
          />
        </div>

        <div className="grid gap-2">
          <Label htmlFor="region">S3 region (optional)</Label>
          <Input
            id="region"
            value={region}
            onChange={(e) => setRegion(e.target.value)}
            placeholder="garage / us-east-1 / ..."
          />
        </div>

        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" disabled={isDisabled}>
            {create.isPending ? "Adding..." : "Add region"}
          </Button>
          <Button type="button" variant="outline" onClick={() => navigate({ to: "/files" })} disabled={create.isPending}>
            Cancel
          </Button>
        </div>
      </form>
    </div>
  );
}
