import { Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { useBucketLifecycle, usePutBucketLifecycle } from "@/shared/api/queries";
import type { LifecycleRule } from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

/**
 * LifecycleSection renders the bucket-detail lifecycle panel.
 *
 * Three branches:
 *   - capabilities.supported === false → gray pill "Lifecycle
 *     policies not supported on this driver" (Garage v1's stub
 *     branch — the operator sees an honest tell on basement.pq.io's
 *     classe cluster).
 *   - supported, no rules → empty-state CTA pointing at the
 *     /lifecycle/new route.
 *   - supported, with rules → table-ish list with human-readable
 *     descriptions + Edit/Delete per row.
 *
 * Each rule is paraphrased to "After N days, <action> objects with
 * prefix <p>" so a 2am operator doesn't have to mentally translate
 * field names. The Delete action mutates the rules list in-place
 * (omitting the target rule) and PUTs the resulting policy back.
 */
export function LifecycleSection({ cid, bid }: { cid: string; bid: string }) {
  const { data, isLoading, error } = useBucketLifecycle(cid, bid);
  const putMutation = usePutBucketLifecycle(cid, bid);
  const queryClient = useQueryClient();

  if (isLoading) {
    return (
      <section className="space-y-3" data-testid="lifecycle-section">
        <SectionHeader cid={cid} bid={bid} supported={false} canAdd={false} />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-muted-foreground">Loading lifecycle policy…</p>
        </div>
      </section>
    );
  }

  if (error || !data) {
    return (
      <section className="space-y-3" data-testid="lifecycle-section">
        <SectionHeader cid={cid} bid={bid} supported={false} canAdd={false} />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-destructive">Couldn't load lifecycle policy.</p>
        </div>
      </section>
    );
  }

  const supported = data.capabilities.supported;

  if (!supported) {
    return (
      <section className="space-y-3" data-testid="lifecycle-section">
        <SectionHeader cid={cid} bid={bid} supported={false} canAdd={false} />
        <div className="rounded-lg border bg-card p-6 flex items-center gap-3">
          <Badge variant="secondary" className="text-xs">
            Unsupported
          </Badge>
          <p className="text-sm text-muted-foreground">
            Lifecycle policies not supported on this driver.
          </p>
        </div>
      </section>
    );
  }

  const handleDelete = (ruleId: string) => {
    const remaining = data.rules.filter((r) => r.id !== ruleId);
    putMutation.mutate(
      { rules: remaining },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({
            queryKey: ["admin", "clusters", cid, "buckets", bid, "lifecycle"],
          });
        },
      },
    );
  };

  return (
    <section className="space-y-3" data-testid="lifecycle-section">
      <SectionHeader cid={cid} bid={bid} supported={true} canAdd={true} />
      {data.rules.length === 0 ? (
        <div className="rounded-lg border bg-card p-6">
          <EmptyState
            icon="key"
            title="No lifecycle rules"
            description='Use "+ Add rule" to expire objects, transition them to cold storage, or cancel stale uploads.'
          />
        </div>
      ) : (
        <div className="rounded-lg border bg-card divide-y">
          {data.rules.map((rule) => (
            <div
              key={rule.id || `${rule.status}-${rule.prefix ?? ""}`}
              className="flex items-center justify-between gap-3 p-4"
            >
              <div className="flex-1 space-y-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium truncate">{rule.id || "(unnamed rule)"}</span>
                  <Badge
                    variant={rule.status === "Enabled" ? "default" : "secondary"}
                    className="text-xs"
                  >
                    {rule.status}
                  </Badge>
                </div>
                <p className="text-sm text-muted-foreground">{describeRule(rule)}</p>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <Link
                  to="/admin/clusters/$cid/buckets/$id/lifecycle/$ruleId/edit"
                  params={{ cid, id: bid, ruleId: rule.id }}
                  className="inline-flex items-center justify-center rounded-lg border border-border bg-background hover:bg-muted h-7 px-2.5 text-[0.8rem] font-medium transition-colors"
                >
                  Edit
                </Link>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={putMutation.isPending}
                  onClick={() => handleDelete(rule.id)}
                >
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function SectionHeader({
  cid,
  bid,
  supported,
  canAdd,
}: {
  cid: string;
  bid: string;
  supported: boolean;
  canAdd: boolean;
}) {
  return (
    <div className="flex items-center justify-between">
      <h2 className="text-sm font-medium text-muted-foreground">Lifecycle</h2>
      {supported && canAdd && (
        <Link
          to="/admin/clusters/$cid/buckets/$id/lifecycle/new"
          params={{ cid, id: bid }}
          className="inline-flex items-center justify-center rounded-lg border border-border bg-background hover:bg-muted h-7 px-2.5 text-[0.8rem] font-medium transition-colors"
        >
          + Add rule
        </Link>
      )}
    </div>
  );
}

// describeRule turns a flat rule into a one-line human description.
// Operators see "After 30 days, delete objects with prefix logs/"
// instead of a field-by-field dump. Composed in two passes so a rule
// with multiple actions (e.g. transition + abort-mpu) reads as a
// comma-separated list.
export function describeRule(rule: LifecycleRule): string {
  const actions: string[] = [];
  if (rule.expirationDays != null) {
    actions.push(`delete objects after ${rule.expirationDays} day${plural(rule.expirationDays)}`);
  }
  if (rule.transitionDays != null && rule.transitionTier) {
    actions.push(
      `move objects to ${rule.transitionTier} after ${rule.transitionDays} day${plural(rule.transitionDays)}`,
    );
  }
  if (rule.noncurrentDays != null) {
    actions.push(
      `delete noncurrent versions after ${rule.noncurrentDays} day${plural(rule.noncurrentDays)}`,
    );
  }
  if (rule.abortMultipartDays != null) {
    actions.push(
      `cancel incomplete uploads after ${rule.abortMultipartDays} day${plural(rule.abortMultipartDays)}`,
    );
  }

  const verb = actions.length === 0 ? "(no actions configured)" : actions.join("; ");
  const scope = rule.prefix ? ` with prefix \`${rule.prefix}\`` : "";
  return capitalize(verb) + scope;
}

function plural(n: number): string {
  return n === 1 ? "" : "s";
}

function capitalize(s: string): string {
  if (!s) return s;
  return s.charAt(0).toUpperCase() + s.slice(1);
}
