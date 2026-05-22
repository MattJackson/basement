import { createFileRoute, Link } from "@tanstack/react-router";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { McpConfigSection } from "@/shared/ui/McpConfigSection";

import { useServiceAccount } from "@/shared/api/queries";
import { humanizeTime } from "@/shared/lib/format";
import { saStatus } from "@/routes/admin/service-accounts/index";

// v1.8.0d — /admin/service-accounts/$id detail page.
//
// Lightweight read-only view of one service account. The two paths
// that need the row's metadata after the mint-once dialog has closed
// are (a) audit attribution lookups ("which SA is sa:xyz?") and
// (b) the MCP config snippet — same one the mint dialog shows, but
// with the plaintext secret gone, so the YAML carries a
// `<SECRET_FROM_ROTATE>` placeholder and a hint to rotate to refill
// it.
//
// Rotation + revocation deliberately stay on the list page's row
// dropdown for v1.8.0d — keeping this detail page focused on the
// "reveal config" use case avoids growing a second affordance for the
// shown-once secret flow. A future cycle can lift those actions into
// the header if usage shows operators land here first.
//
// Parent layout (service-accounts.tsx) owns the admin chrome; this
// route returns just the page body.
export const Route = createFileRoute("/admin/service-accounts/$id")({
  component: ServiceAccountDetailPage,
});

function ServiceAccountDetailPage() {
  const { id } = Route.useParams();
  const { data: sa, isLoading, error } = useServiceAccount(id);

  const header = (
    <div className="space-y-2">
      <div className="text-xs text-muted-foreground">
        <Link to="/admin/service-accounts" className="hover:text-foreground">
          ← Back to service accounts
        </Link>
      </div>
      <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
        {isLoading ? <Skeleton className="h-8 w-48" /> : (sa?.name ?? "Service account")}
      </h1>
    </div>
  );

  if (isLoading) {
    return (
      <div className="space-y-6">
        {header}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Loading…</CardTitle>
          </CardHeader>
          <CardContent>
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-2/3 mt-2" />
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error || !sa) {
    return (
      <div className="space-y-6">
        {header}
        <ErrorBanner
          message={
            error?.message ??
            "Service account not found or you don't have access."
          }
        />
      </div>
    );
  }

  const status = saStatus(sa);

  return (
    <div className="space-y-6">
      {header}

      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center justify-between gap-2">
            <span>Identity</span>
            <StatusBadge status={status} />
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <Field label="Access Key ID">
            <code className="font-mono text-xs break-all">{sa.accessKeyId}</code>
          </Field>
          <Field label="Created">
            <span className="text-muted-foreground">
              {humanizeTime(sa.createdAt)}
            </span>
          </Field>
          <Field label="Last used">
            <span className="text-muted-foreground">
              {sa.lastUsedAt ? humanizeTime(sa.lastUsedAt) : "never"}
            </span>
          </Field>
          <Field label="Expires">
            <span className="text-muted-foreground">
              {sa.expiresAt ? humanizeTime(sa.expiresAt) : "never"}
            </span>
          </Field>
          {sa.revokedAt && (
            <Field label="Revoked">
              <span className="text-destructive">
                {humanizeTime(sa.revokedAt)}
              </span>
            </Field>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Capabilities</CardTitle>
        </CardHeader>
        <CardContent>
          {sa.capabilities.length === 0 ? (
            <p className="text-sm text-muted-foreground italic">
              No capabilities granted.
            </p>
          ) : (
            <ul className="space-y-1" data-testid="sa-capabilities-list">
              {sa.capabilities.map((c) => (
                <li
                  key={`${c.id}@${c.scope}`}
                  className="flex flex-wrap items-center gap-2 text-xs"
                >
                  <Badge variant="outline" className="font-mono text-[10px]">
                    {c.id}
                  </Badge>
                  <span className="text-muted-foreground">@</span>
                  <code className="font-mono text-xs break-all">{c.scope}</code>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Use with MCP</CardTitle>
        </CardHeader>
        <CardContent>
          {/* v1.8.0d — Same shared component the mint dialog renders.
              We pass secret=null so the YAML carries the
              <SECRET_FROM_ROTATE> placeholder + the rotate hint
              renders underneath. Operators rotate on the list page's
              row dropdown to get a fresh plaintext value. */}
          <McpConfigSection accessKeyId={sa.accessKeyId} secret={null} />
        </CardContent>
      </Card>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[8rem_1fr] gap-2 items-baseline">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div>{children}</div>
    </div>
  );
}

function StatusBadge({ status }: { status: "revoked" | "expired" | "active" }) {
  if (status === "revoked") {
    return (
      <Badge
        variant="outline"
        className="border-destructive/40 bg-destructive/10 text-destructive"
      >
        Revoked
      </Badge>
    );
  }
  if (status === "expired") {
    return (
      <Badge
        variant="outline"
        className="border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400"
      >
        Expired
      </Badge>
    );
  }
  return (
    <Badge
      variant="outline"
      className="border-green-500/40 bg-green-500/10 text-green-700 dark:text-green-400"
    >
      Active
    </Badge>
  );
}
