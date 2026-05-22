import { Fragment, useMemo, useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

import {
  usePolicies,
  useCreateServiceAccount,
  type ServiceAccountCapability,
  type ServiceAccountWithSecret,
  type PolicyCapability,
} from "@/shared/api/queries";
import { useElevationGuard } from "@/shared/auth/elevation";
import { SecretShownOnceDialog } from "@/shared/ui/SecretShownOnceDialog";

// v1.7.0c — /admin/service-accounts/new
//
// Route page (not modal) per popups-max-2-fields: the capability
// picker alone is a multi-row checkbox grid + per-capability scope
// editor, so this MUST be a page.
//
// On submit:
//   POST /api/v1/admin/service-accounts → {serviceAccount, secret}
//   → show <SecretShownOnceDialog>
//   → Done navigates back to the list (the list re-fetches via
//     the invalidated query key).
//
// NOTE: parent layout (service-accounts.tsx) owns the admin chrome.
export const Route = createFileRoute("/admin/service-accounts/new")({
  component: NewServiceAccountPage,
});

// Expiry presets — the cycle prompt's "Never / 1 month / 6 months /
// 1 year / Custom date" set. Values are the number of days from now;
// `null` means "never expires"; "custom" defers to the date input.
const EXPIRY_PRESETS = [
  { id: "never", label: "Never", days: null as number | null },
  { id: "1m", label: "1 month", days: 30 },
  { id: "6m", label: "6 months", days: 30 * 6 },
  { id: "1y", label: "1 year", days: 365 },
  { id: "custom", label: "Custom date", days: null as number | null },
];

function NewServiceAccountPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const runWithElevation = useElevationGuard();
  const policies = usePolicies();
  const create = useCreateServiceAccount();

  const [name, setName] = useState("");
  const [expiryPreset, setExpiryPreset] = useState<string>("never");
  const [customExpiry, setCustomExpiry] = useState<string>(""); // YYYY-MM-DD
  // Selected capability → scope. The Map keeps insertion order so
  // newly-added rows land at the bottom of the per-cap scope editor.
  const [selected, setSelected] = useState<Map<string, string>>(new Map());
  const [capSearch, setCapSearch] = useState("");

  const [result, setResult] = useState<ServiceAccountWithSecret | null>(null);

  const capabilities: PolicyCapability[] = useMemo(
    () => policies.data?.capabilities ?? [],
    [policies.data?.capabilities],
  );

  // Group + filter the capability picker. We exclude the catch-all
  // wildcard from the picker — granting `*:*` via an SA would defeat
  // the point of scoping, and operators who really need it should
  // know enough to type it by hand (out of scope for v1.7.0c).
  const grouped = useMemo(() => {
    const byDomain = new Map<string, PolicyCapability[]>();
    for (const c of capabilities) {
      if (c.id === "*:*") continue;
      const needle = capSearch.trim().toLowerCase();
      if (
        needle &&
        !c.id.toLowerCase().includes(needle) &&
        !c.description.toLowerCase().includes(needle)
      ) {
        continue;
      }
      const domain = c.id.split(":")[0] ?? "other";
      const arr = byDomain.get(domain) ?? [];
      arr.push(c);
      byDomain.set(domain, arr);
    }
    return Array.from(byDomain.entries()).sort(([a], [b]) =>
      a.localeCompare(b),
    );
  }, [capabilities, capSearch]);

  const toggleCapability = (capId: string) => {
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(capId)) {
        next.delete(capId);
      } else {
        next.set(capId, defaultScopeFor(capId));
      }
      return next;
    });
  };

  const setScope = (capId: string, scope: string) => {
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(capId)) next.set(capId, scope);
      return next;
    });
  };

  const expiresAtISO = (): string | null | undefined => {
    if (expiryPreset === "never") return null;
    if (expiryPreset === "custom") {
      if (!customExpiry) return undefined; // sentinel: validation will catch
      const d = new Date(`${customExpiry}T23:59:59Z`);
      if (isNaN(d.getTime())) return undefined;
      return d.toISOString();
    }
    const preset = EXPIRY_PRESETS.find((p) => p.id === expiryPreset);
    if (!preset?.days) return null;
    const d = new Date(Date.now() + preset.days * 24 * 60 * 60 * 1000);
    return d.toISOString();
  };

  const canSubmit = (() => {
    if (!name.trim()) return false;
    if (selected.size === 0) return false;
    if (expiryPreset === "custom" && !customExpiry) return false;
    return true;
  })();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;

    const expiresAt = expiresAtISO();
    if (expiresAt === undefined) {
      toast.error("Pick a valid custom expiry date");
      return;
    }

    const caps: ServiceAccountCapability[] = Array.from(selected.entries()).map(
      ([id, scope]) => ({ id, scope }),
    );

    try {
      const res = await runWithElevation(() =>
        create.mutateAsync({
          name: name.trim(),
          capabilities: caps,
          scopes: [],
          expiresAt: expiresAt ?? null,
        }),
      );
      queryClient.invalidateQueries({ queryKey: ["admin", "service-accounts"] });
      setResult(res);
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <div className="space-y-6">
      <header>
        <div className="text-xs text-muted-foreground">
          <Link to="/admin/service-accounts" className="hover:text-foreground">
            ← Back to service accounts
          </Link>
        </div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight mt-1">
          Mint a service account
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          Pick a name, an expiry, and the capabilities the credential
          carries. The secret is shown exactly once after submission.
        </p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-6" data-testid="new-sa-form">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Identity</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                Name
              </label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. backup-cron, ci-deploy"
                autoFocus
                data-testid="sa-name-input"
              />
              <p className="text-xs text-muted-foreground">
                Letters, numbers, underscore, and dash. 3–64 chars.
              </p>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Expiry</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex flex-wrap gap-2">
              {EXPIRY_PRESETS.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => setExpiryPreset(p.id)}
                  data-testid={`expiry-preset-${p.id}`}
                  className={
                    expiryPreset === p.id
                      ? "rounded-md border-2 border-primary bg-primary/5 px-3 py-1 text-sm font-medium"
                      : "rounded-md border bg-muted/30 px-3 py-1 text-sm hover:bg-muted"
                  }
                >
                  {p.label}
                </button>
              ))}
            </div>
            {expiryPreset === "custom" && (
              <div className="space-y-1 pt-2">
                <label className="text-xs font-medium text-muted-foreground">
                  Expires on
                </label>
                <Input
                  type="date"
                  value={customExpiry}
                  onChange={(e) => setCustomExpiry(e.target.value)}
                  data-testid="sa-custom-expiry-input"
                  className="max-w-xs"
                />
              </div>
            )}
            <p className="text-xs text-muted-foreground">
              After the expiry date the bearer middleware refuses the
              credential. Pair "Never" with rotation discipline.
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center justify-between gap-2">
              <span>Capabilities</span>
              <span className="text-xs font-normal text-muted-foreground">
                {selected.size} selected
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Input
              placeholder="Search capabilities..."
              value={capSearch}
              onChange={(e) => setCapSearch(e.target.value)}
              data-testid="sa-cap-search"
            />

            {policies.isLoading ? (
              <div className="text-sm text-muted-foreground">
                Loading capability registry...
              </div>
            ) : policies.error ? (
              <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                Couldn't load capability registry: {policies.error.message}
              </div>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                {grouped.length === 0 ? (
                  <div className="col-span-full text-sm text-muted-foreground">
                    No capabilities match this search.
                  </div>
                ) : (
                  grouped.map(([domain, caps]) => (
                    <div
                      key={domain}
                      className="rounded-md border bg-muted/20 p-2"
                    >
                      <div className="mb-1 font-mono text-[10px] uppercase text-muted-foreground">
                        {domain}
                      </div>
                      <div className="space-y-1">
                        {caps.map((c) => (
                          <label
                            key={c.id}
                            className="flex items-start gap-2 text-xs cursor-pointer"
                          >
                            <Checkbox
                              checked={selected.has(c.id)}
                              onCheckedChange={() => toggleCapability(c.id)}
                              data-testid={`cap-checkbox-${c.id}`}
                            />
                            <span>
                              <span className="font-mono">
                                {c.id.split(":")[1] ?? c.id}
                              </span>
                              <span className="ml-1 text-muted-foreground">
                                — {c.description}
                              </span>
                            </span>
                          </label>
                        ))}
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {selected.size > 0 && (
              <div className="space-y-2 pt-2">
                <div className="text-xs font-medium text-muted-foreground">
                  Per-capability scope
                </div>
                <div className="space-y-1">
                  {Array.from(selected.entries()).map(([id, scope]) => (
                    <Fragment key={id}>
                      <div className="grid grid-cols-[auto_1fr_auto] items-center gap-2">
                        <Badge variant="outline" className="font-mono text-[10px]">
                          {id}
                        </Badge>
                        <Input
                          value={scope}
                          onChange={(e) => setScope(id, e.target.value)}
                          placeholder={defaultScopeFor(id)}
                          className="font-mono text-xs"
                          data-testid={`cap-scope-${id}`}
                        />
                        <Button
                          variant="ghost"
                          size="sm"
                          type="button"
                          onClick={() => toggleCapability(id)}
                        >
                          Remove
                        </Button>
                      </div>
                    </Fragment>
                  ))}
                </div>
                <p className="text-xs text-muted-foreground">
                  Scope grammar: <code className="font-mono">host:*</code>,{" "}
                  <code className="font-mono">cluster:*</code>,{" "}
                  <code className="font-mono">cluster:&#123;cid&#125;</code>,{" "}
                  <code className="font-mono">bucket:&#123;cid&#125;:*</code>,{" "}
                  <code className="font-mono">bucket:&#123;cid&#125;:&#123;bid&#125;</code>,{" "}
                  <code className="font-mono">key:&#123;cid&#125;:*</code>,{" "}
                  <code className="font-mono">key:&#123;cid&#125;:&#123;kid&#125;</code>.
                </p>
              </div>
            )}
          </CardContent>
        </Card>

        {create.error && (
          <div
            role="alert"
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
          >
            {create.error.message.replace(/^[A-Z_]+:\s*/, "")}
          </div>
        )}

        <div className="flex items-center justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate({ to: "/admin/service-accounts" })}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={!canSubmit || create.isPending}
            data-testid="sa-submit-button"
          >
            {create.isPending ? "Minting…" : "Mint service account"}
          </Button>
        </div>
      </form>

      {result && (
        <SecretShownOnceDialog
          result={result}
          mode="create"
          onDone={() => {
            setResult(null);
            navigate({ to: "/admin/service-accounts" });
          }}
        />
      )}
    </div>
  );
}

// defaultScopeFor returns the safest sensible scope hint for a
// capability — host caps default to host:*, cluster caps to cluster:*,
// bucket / key caps to a templated form the operator narrows. Same
// six-form grammar the backend enforces in
// validateServiceAccountScope; the FE just pre-populates.
export function defaultScopeFor(capId: string): string {
  const domain = capId.split(":")[0];
  switch (domain) {
    case "host":
      return "host:*";
    case "cluster":
      return "cluster:*";
    case "bucket":
      return "bucket:{cid}:*";
    case "key":
      return "key:{cid}:*";
    default:
      return "host:*";
  }
}
