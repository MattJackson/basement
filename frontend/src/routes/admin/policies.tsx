// ADR-0001 cycle v0.9.0g — /admin/policies matrix editor.
//
// Three stacked sections (per the cycle prompt + popups-max-2-fields
// memory):
//
//   1. Capabilities reference — collapsible read-only table from the
//      compiled Registry. Lets the admin see the verbs the system
//      knows about before mapping them onto roles.
//   2. Roles editor — one card per role. Editing a role's caps is an
//      INLINE checkbox grid (not a dialog) because the matrix is the
//      multi-field thing this page is FOR. Editing label+description
//      stays inline too; cards expand into edit mode on demand.
//   3. Assignments — table with an inline "+ Assign role" form at the
//      top (3 fields: user, role, scope) per the prompt — popups would
//      cap us at 2 fields and assignments are the most critical low-
//      frequency action this page handles, so an inline form is the
//      right call.
//
// Seed roles render with a "Seed" badge, the Delete button is disabled
// with a hover tooltip explaining why; capabilities of seed roles can
// still be edited (preserving Seed flag is the enforcer's job).

import { Fragment, useMemo, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Checkbox } from "@/components/ui/checkbox";

import { adminPage } from "@/shared/layout/adminPage";
import {
  usePolicies,
  useUpsertRole,
  useDeleteRole,
  useAssignRole,
  useUnassignRole,
  useSimulatePolicy,
  type PolicyCapability,
  type PolicyRole,
  type PolicyAssignment,
  type SimulateResponse,
} from "@/shared/api/queries";
import { useElevationGuard } from "@/shared/auth/elevation";

export const Route = createFileRoute("/admin/policies")({
  component: adminPage(PoliciesPage),
});

function PoliciesPage() {
  const { data, isLoading, error } = usePolicies();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px] text-muted-foreground">
        Loading policies...
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
        Failed to load policies: {(error as Error | undefined)?.message ?? "unknown error"}
      </div>
    );
  }

  return (
    <TooltipProvider>
      <div className="space-y-8">
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            Policies
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            Roles, capabilities, and per-user assignments. Edit the matrix to
            grant or revoke access without re-deploying.
          </p>
        </header>

        <DeprecationBanner />
        <CapabilitiesPane capabilities={data.capabilities} />
        <RolesPane roles={data.roles} capabilities={data.capabilities} />
        <AssignmentsPane
          assignments={data.assignments}
          roles={data.roles}
        />
        <SimulatorPane
          assignments={data.assignments}
          capabilities={data.capabilities}
        />
      </div>
    </TooltipProvider>
  );
}

// DeprecationBanner — ADR-0002 (v1.1.0f) primer.
//
// Renders once at the top of /admin/policies so operators understand,
// before they scan the matrix, that the legacy bucket_user role's
// gates retired in v1.1.0e. New assignments to bucket_user have no
// effect; the path forward is the region keychain. Banner stays even
// when no deprecated roles are present in the matrix (a custom
// deployment that wiped the seed) because the message is also about
// the architectural model, not just one role's status.
function DeprecationBanner() {
  return (
    <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
      <strong className="font-semibold">
        Bucket-level access is now controlled by S3 key assignment on the
        cluster admin side, not by basement roles.
      </strong>{" "}
      When a user connects a region (
      <code className="font-mono text-xs">/files/regions/new</code>), they
      bring an S3 key that already has the backend's bucket grants attached.
      basement no longer enforces per-bucket access — the backend does.
      The <code className="font-mono text-xs">bucket_user</code> role
      remains for backward compatibility but new assignments will have no
      effect.
    </div>
  );
}

// --- Capabilities pane ------------------------------------------------

function CapabilitiesPane({ capabilities }: { capabilities: PolicyCapability[] }) {
  // Collapsed by default — the reference table is for occasional lookup,
  // not a daily-driver surface.
  const [open, setOpen] = useState(false);

  // Group by domain (prefix before `:`) so 30+ rows scan as 6 groups.
  const grouped = useMemo(() => {
    const byDomain = new Map<string, PolicyCapability[]>();
    for (const c of capabilities) {
      const domain = c.id.split(":")[0] ?? "other";
      const arr = byDomain.get(domain) ?? [];
      arr.push(c);
      byDomain.set(domain, arr);
    }
    return Array.from(byDomain.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [capabilities]);

  return (
    <section>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 text-lg font-medium tracking-tight hover:text-primary transition-colors"
      >
        <span>{open ? "▼" : "▶"}</span>
        Capability reference
        <span className="text-sm text-muted-foreground font-normal">
          ({capabilities.length} verbs)
        </span>
      </button>

      {open && (
        <div className="mt-3 rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-64">Capability ID</TableHead>
                <TableHead>Description</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {grouped.map(([domain, caps]) => (
                <Fragment key={domain}>
                  <TableRow>
                    <TableCell colSpan={2} className="bg-muted/40 font-mono text-xs uppercase text-muted-foreground">
                      {domain}
                    </TableCell>
                  </TableRow>
                  {caps.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell className="font-mono text-xs">{c.id}</TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {c.description}
                      </TableCell>
                    </TableRow>
                  ))}
                </Fragment>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  );
}

// --- Roles pane -------------------------------------------------------

function RolesPane({
  roles,
  capabilities,
}: {
  roles: PolicyRole[];
  capabilities: PolicyCapability[];
}) {
  const [addOpen, setAddOpen] = useState(false);

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-medium tracking-tight">Roles</h2>
        <Button size="sm" onClick={() => setAddOpen((v) => !v)}>
          {addOpen ? "Cancel" : "+ Add custom role"}
        </Button>
      </div>

      {addOpen && (
        <AddRoleCard
          capabilities={capabilities}
          onDone={() => setAddOpen(false)}
        />
      )}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {roles.map((r) => (
          <RoleCard key={r.id} role={r} capabilities={capabilities} />
        ))}
      </div>
    </section>
  );
}

function AddRoleCard({
  capabilities,
  onDone,
}: {
  capabilities: PolicyCapability[];
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const upsert = useUpsertRole();
  // v1.3.0a.3: policy:edit_matrix is ADMIN-min. Wrap so the modal
  // opens + the mutation retries on success.
  const runWithElevation = useElevationGuard();
  const [id, setId] = useState("");
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [selectedCaps, setSelectedCaps] = useState<Set<string>>(new Set());

  const handleSave = async () => {
    if (!id.trim()) {
      toast.error("Role id is required");
      return;
    }
    try {
      await runWithElevation(() =>
        upsert.mutateAsync({
          id: id.trim(),
          label: label.trim() || id.trim(),
          description: description.trim(),
          capabilities: Array.from(selectedCaps),
        }),
      );
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success(`Role ${id} created`);
      onDone();
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <Card className="border-primary/40">
      <CardHeader>
        <CardTitle>New custom role</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-2 sm:grid-cols-2">
          <div className="space-y-1">
            <label className="text-xs font-medium text-muted-foreground">
              ID (snake_case)
            </label>
            <Input
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="e.g. backup_operator"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-muted-foreground">Label</label>
            <Input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="Backup Operator"
            />
          </div>
        </div>
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Description</label>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="What this role is for"
          />
        </div>
        <CapabilityCheckboxes
          capabilities={capabilities}
          selected={selectedCaps}
          onChange={setSelectedCaps}
        />
        <div className="flex items-center justify-end gap-2 pt-2">
          <Button variant="outline" size="sm" onClick={onDone}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSave} disabled={upsert.isPending}>
            {upsert.isPending ? "Saving..." : "Create role"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function RoleCard({
  role,
  capabilities,
}: {
  role: PolicyRole;
  capabilities: PolicyCapability[];
}) {
  const queryClient = useQueryClient();
  const upsert = useUpsertRole();
  const del = useDeleteRole();
  // v1.3.0a.3: policy:edit_matrix is ADMIN-min, policy role delete is
  // ELEVATED-min. Both go through the same wrapper.
  const runWithElevation = useElevationGuard();

  const [editing, setEditing] = useState(false);
  const [label, setLabel] = useState(role.label);
  const [description, setDescription] = useState(role.description);
  const [selected, setSelected] = useState<Set<string>>(new Set(role.capabilities));

  // Detect superuser wildcard so we don't render 30+ checked boxes for
  // the host_admin row (it'd look broken). We show a single chip and
  // bail on editing capabilities for `*:*` roles.
  const isWildcard = role.capabilities.includes("*:*");

  const handleSave = async () => {
    try {
      await runWithElevation(() =>
        upsert.mutateAsync({
          id: role.id,
          label: label.trim() || role.id,
          description: description.trim(),
          capabilities: Array.from(selected),
        }),
      );
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success(`Role ${role.id} saved`);
      setEditing(false);
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  const handleDelete = async () => {
    if (!confirm(`Delete role ${role.id}? Assignments using it will be cleared.`)) {
      return;
    }
    try {
      await runWithElevation(() => del.mutateAsync(role.id));
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success(`Role ${role.id} deleted`);
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <CardTitle className="truncate">{role.label || role.id}</CardTitle>
            <div className="mt-0.5 text-xs font-mono text-muted-foreground truncate">
              {role.id}
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-1">
            {role.deprecated && (
              <Badge
                variant="outline"
                className="border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400"
              >
                Deprecated
              </Badge>
            )}
            {role.seed && (
              <Badge variant="secondary">Seed</Badge>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {editing ? (
          <>
            {role.deprecated && (
              <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-800 dark:text-amber-300">
                This role is deprecated. New assignments have no effect — use{" "}
                <code className="font-mono">/files/regions/new</code> instead.
                Bucket-level access is decided by the backend S3 key attached to
                the user's UserRegion, not by basement policy. Existing
                assignments remain valid for back-compat but should be revoked
                once users have migrated to the region keychain.
              </div>
            )}
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">Label</label>
              <Input value={label} onChange={(e) => setLabel(e.target.value)} />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                Description
              </label>
              <Input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
            {isWildcard ? (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-2 text-xs">
                This role holds the <code className="font-mono">*:*</code>{" "}
                superuser wildcard. To narrow it, remove the wildcard and pick
                specific capabilities below.
              </div>
            ) : null}
            <CapabilityCheckboxes
              capabilities={capabilities}
              selected={selected}
              onChange={setSelected}
            />
            <div className="flex items-center justify-end gap-2 pt-2">
              <Button variant="outline" size="sm" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button size="sm" onClick={handleSave} disabled={upsert.isPending}>
                {upsert.isPending ? "Saving..." : "Save"}
              </Button>
            </div>
          </>
        ) : (
          <>
            {role.description && (
              <p className="text-sm text-muted-foreground">{role.description}</p>
            )}
            <div className="flex flex-wrap gap-1.5">
              {role.capabilities.length === 0 && (
                <span className="text-xs text-muted-foreground italic">
                  No capabilities
                </span>
              )}
              {role.capabilities.map((c) => (
                <Badge
                  key={c}
                  variant="outline"
                  className="font-mono text-[10px]"
                >
                  {c}
                </Badge>
              ))}
            </div>
            <div className="flex items-center justify-end gap-2 pt-1">
              <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                Edit
              </Button>
              {role.seed ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span>
                      <Button variant="destructive" size="sm" disabled>
                        Delete
                      </Button>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>
                    Seed roles can be edited but not deleted — prevents
                    accidental lockout.
                  </TooltipContent>
                </Tooltip>
              ) : (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={handleDelete}
                  disabled={del.isPending}
                >
                  Delete
                </Button>
              )}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function CapabilityCheckboxes({
  capabilities,
  selected,
  onChange,
}: {
  capabilities: PolicyCapability[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  // Group by domain so the checkbox grid scans as columns rather than
  // a flat list of 30. Edits mutate a fresh Set so React notices.
  const grouped = useMemo(() => {
    const byDomain = new Map<string, PolicyCapability[]>();
    for (const c of capabilities) {
      const domain = c.id.split(":")[0] ?? "other";
      const arr = byDomain.get(domain) ?? [];
      arr.push(c);
      byDomain.set(domain, arr);
    }
    return Array.from(byDomain.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [capabilities]);

  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    onChange(next);
  };

  return (
    <div className="space-y-2">
      <div className="text-xs font-medium text-muted-foreground">Capabilities</div>
      <div className="grid gap-3 sm:grid-cols-2">
        {grouped.map(([domain, caps]) => (
          <div key={domain} className="rounded-md border bg-muted/20 p-2">
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
                    onCheckedChange={() => toggle(c.id)}
                  />
                  <span>
                    <span className="font-mono">{c.id.split(":")[1] ?? c.id}</span>
                    <span className="ml-1 text-muted-foreground">
                      — {c.description}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// --- Assignments pane -------------------------------------------------

function AssignmentsPane({
  assignments,
  roles,
}: {
  assignments: PolicyAssignment[];
  roles: PolicyRole[];
}) {
  const queryClient = useQueryClient();
  const assign = useAssignRole();
  const unassign = useUnassignRole();
  // v1.3.0a.3: policy:assign_role / unassign_role are ADMIN-min.
  // Wrap so the modal opens + the mutation retries on success.
  const runWithElevation = useElevationGuard();

  const [userFilter, setUserFilter] = useState("");
  const [roleFilter, setRoleFilter] = useState("");

  // Inline add form state — three fields means a dialog would violate
  // popups-max-2-fields. Keeping it inline at the top of the table.
  const [newUser, setNewUser] = useState("");
// Default the role picker to the first non-deprecated role so the
	// form doesn't open already-blocked.
	const defaultRole = roles.find((r) => !r.deprecated)?.id ?? roles[0]?.id ?? "";
  const [newRole, setNewRole] = useState(defaultRole);
  const [newScope, setNewScope] = useState("");

  const selectedRole = roles.find((r) => r.id === newRole);
  const selectedIsDeprecated = !!selectedRole?.deprecated;

  const filtered = useMemo(() => {
    return assignments.filter((a) => {
      if (userFilter && !a.userId.toLowerCase().includes(userFilter.toLowerCase())) {
        return false;
      }
      if (roleFilter && a.roleId !== roleFilter) {
        return false;
      }
      return true;
    });
  }, [assignments, userFilter, roleFilter]);

  const handleAssign = async () => {
    if (!newUser.trim() || !newRole || !newScope.trim()) {
      toast.error("All three fields are required");
      return;
    }
// v2.0.0a: bucket_user removed entirely — no assignments possible.
	// Belt-and-suspenders guard alongside the disabled button ensures
	// keyboard-triggered submits or stale state can't sneak a deprecated
	// assignment through. The disabled <Button> above is the primary UI signal;
	// this is the last-resort check.
	if (selectedIsDeprecated) {
      toast.error(
        `Role ${newRole} is deprecated — new assignments have no effect. Use /files/regions/new instead.`,
      );
      return;
    }
    try {
      await runWithElevation(() =>
        assign.mutateAsync({
          userId: newUser.trim(),
          roleId: newRole,
          scope: newScope.trim(),
        }),
      );
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success(`Assigned ${newRole} to ${newUser}`);
      setNewUser("");
      setNewScope("");
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  const handleRevoke = async (a: PolicyAssignment) => {
    if (
      !confirm(
        `Revoke ${a.roleId} from ${a.userId} on ${a.scope}? They lose those capabilities immediately.`,
      )
    ) {
      return;
    }
    try {
      await runWithElevation(() => unassign.mutateAsync(a));
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success("Assignment revoked");
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <section className="space-y-3">
      <h2 className="text-lg font-medium tracking-tight">Assignments</h2>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Assign a role</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-2 sm:grid-cols-[2fr_2fr_3fr_auto] sm:items-end">
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                User ID
              </label>
              <Input
                value={newUser}
                onChange={(e) => setNewUser(e.target.value)}
                placeholder="e.g. wife"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">Role</label>
              <select
                value={newRole}
                onChange={(e) => setNewRole(e.target.value)}
                className="h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm"
              >
                {roles.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label || r.id}
                    {r.deprecated ? " (deprecated)" : ""}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                Scope
              </label>
              <Input
                value={newScope}
                onChange={(e) => setNewScope(e.target.value)}
                placeholder="e.g. bucket:cid-abc:family-photos"
              />
            </div>
            {selectedIsDeprecated ? (
              // v2.0.0a: bucket_user removed entirely — can't create new
              // assignments to deprecated roles. Tooltip explains why so the
              // operator doesn't think the button is just broken.
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    <Button size="sm" disabled>
                      Assign
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  <code className="font-mono text-[10px]">{newRole}</code> is
                  deprecated — new assignments would have no effect.
                  Bucket-level access is now decided by the backend S3 key
                  attached to a user's UserRegion (
                  <code className="font-mono text-[10px]">/files/regions/new</code>
                  ), not by basement policy. Existing assignments remain
                  visible below and can be revoked for cleanup.
                </TooltipContent>
              </Tooltip>
            ) : (
              <Button size="sm" onClick={handleAssign} disabled={assign.isPending}>
                {assign.isPending ? "Assigning..." : "Assign"}
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">Filter user:</span>
          <Input
            value={userFilter}
            onChange={(e) => setUserFilter(e.target.value)}
            placeholder="contains..."
            className="h-7 w-40"
          />
        </label>
        <label className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">Filter role:</span>
          <select
            value={roleFilter}
            onChange={(e) => setRoleFilter(e.target.value)}
            className="h-7 rounded-lg border border-input bg-transparent px-2 text-sm"
          >
            <option value="">(any)</option>
            {roles.map((r) => (
              <option key={r.id} value={r.id}>
                {r.label || r.id}
              </option>
            ))}
          </select>
        </label>
        <span className="ml-auto text-xs text-muted-foreground">
          {filtered.length} of {assignments.length} assignments
        </span>
      </div>

      <div className="rounded-lg border bg-card overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead className="w-24 text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="text-center text-sm text-muted-foreground py-6"
                >
                  No assignments {assignments.length > 0 ? "match the filters" : "yet"}
                </TableCell>
              </TableRow>
            ) : (
              filtered.map((a, i) => (
                <TableRow key={`${a.userId}|${a.roleId}|${a.scope}|${i}`}>
                  <TableCell className="font-medium">{a.userId}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono text-[10px]">
                      {a.roleId}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {a.scope}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => handleRevoke(a)}
                      disabled={unassign.isPending}
                    >
                      Revoke
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

// --- Simulator pane (POLICY.SIM, v0.9.0j) -----------------------------
//
// "Given this user, can they do X at scope Y, and WHY?" An inline
// panel — three inputs + Run — because operators staring at the
// matrix above want the answer in the same view, not a separate
// route. The popups-max-2-fields doctrine forbids stuffing this into
// a dialog (three fields would violate it), and a route would push
// the user away from the matrix they're trying to debug, so an
// inline section reading from the SAME `data` payload is the right
// call.
//
// The User input is a free-text combobox (a <datalist>) seeded from
// known assignment user IDs — operators commonly want to test users
// who have ZERO assignments ("can the new wife account view family-
// photos before I assign her?") so we don't restrict to enrolled
// users. The Capability input is a strict <select> from the seeded
// registry. The Scope input is free text — scope is the most varied
// axis (host:*, cluster:abc, bucket:cid:bid, …) so a dropdown would
// be wrong; a placeholder + example chips give the same affordance
// without the lock-in.
function SimulatorPane({
  assignments,
  capabilities,
}: {
  assignments: PolicyAssignment[];
  capabilities: PolicyCapability[];
}) {
  const simulate = useSimulatePolicy();

  // Build the user dropdown source from every userId that appears in
  // an assignment. De-dup + sort for stable rendering. Operators can
  // still type a free-text user (e.g. an unassigned account) — this
  // is just an autocomplete hint, not a strict allowlist.
  const knownUsers = useMemo(() => {
    const set = new Set<string>();
    for (const a of assignments) set.add(a.userId);
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [assignments]);

  // Capabilities come back from the API already sorted by id; we keep
  // that order so the dropdown matches the reference table above.
  const [userId, setUserId] = useState("");
  const [capability, setCapability] = useState(capabilities[0]?.id ?? "");
  const [scope, setScope] = useState("");

  // Remember the last successful result so the answer survives input
  // edits — operators routinely tweak one field after a deny to see
  // what would change. Cleared on a fresh Run.
  const [result, setResult] = useState<SimulateResponse | null>(null);

  const handleSimulate = async () => {
    if (!userId.trim() || !capability || !scope.trim()) {
      toast.error("User, capability, and scope are all required");
      return;
    }
    try {
      const resp = await simulate.mutateAsync({
        userId: userId.trim(),
        capability,
        scope: scope.trim(),
      });
      setResult(resp);
    } catch (e) {
      setResult(null);
      toast.error((e as Error).message);
    }
  };

  // Example scopes lifted from ADR-0001's "Scope grammar" table. They
  // double as placeholders AND click-to-fill chips so an operator
  // unfamiliar with the grammar can prime the form quickly.
  const exampleScopes = [
    "host:*",
    "cluster:cid-abc",
    "bucket:cid-abc:lsi",
    "bucket:cid-abc:family-photos",
    "key:cid-abc:somekey",
  ];

  return (
    <section className="space-y-3">
      <div>
        <h2 className="text-lg font-medium tracking-tight">Simulator</h2>
        <p className="text-sm text-muted-foreground mt-0.5">
          What-if inspector: given a user + capability + scope, why does the
          enforcer allow or deny? Pure analysis — no policy is changed.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Run a simulation</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-2 sm:grid-cols-[2fr_2fr_3fr_auto] sm:items-end">
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                User ID
              </label>
              <Input
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                placeholder="e.g. wife"
                list="sim-known-users"
              />
              <datalist id="sim-known-users">
                {knownUsers.map((u) => (
                  <option key={u} value={u} />
                ))}
              </datalist>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                Capability
              </label>
              <select
                value={capability}
                onChange={(e) => setCapability(e.target.value)}
                className="h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm font-mono"
              >
                {capabilities.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.id}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">
                Scope
              </label>
              <Input
                value={scope}
                onChange={(e) => setScope(e.target.value)}
                placeholder="e.g. bucket:cid-abc:family-photos"
                className="font-mono"
              />
            </div>
            <Button size="sm" onClick={handleSimulate} disabled={simulate.isPending}>
              {simulate.isPending ? "Running..." : "Simulate"}
            </Button>
          </div>

          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">Scope examples:</span>
            {exampleScopes.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setScope(s)}
                className="rounded-md border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px] hover:bg-muted transition-colors"
              >
                {s}
              </button>
            ))}
          </div>

          {result && <SimulatorResult result={result} />}
        </CardContent>
      </Card>
    </section>
  );
}

function SimulatorResult({ result }: { result: SimulateResponse }) {
  return (
    <div className="space-y-3 pt-2 border-t">
      <div className="flex items-center gap-3">
        <div
          className={
            result.allowed
              ? "rounded-md bg-green-500/15 text-green-700 dark:text-green-400 px-3 py-1.5 text-sm font-semibold border border-green-500/30"
              : "rounded-md bg-destructive/15 text-destructive px-3 py-1.5 text-sm font-semibold border border-destructive/30"
          }
        >
          {result.allowed ? "Allowed" : "Denied"}
        </div>
        <span className="text-xs text-muted-foreground">
          {result.reasoning.length} reasoning step
          {result.reasoning.length === 1 ? "" : "s"}
          {result.matchingAssignments.length > 0 && (
            <> · {result.matchingAssignments.length} matching assignment{result.matchingAssignments.length === 1 ? "" : "s"}</>
          )}
        </span>
      </div>

      {result.matchingAssignments.length > 0 && (
        <div>
          <div className="text-xs font-medium text-muted-foreground mb-1">
            Matching assignments
          </div>
          <div className="flex flex-wrap gap-1.5">
            {result.matchingAssignments.map((a, i) => (
              <Badge
                key={`${a.userId}|${a.roleId}|${a.scope}|${i}`}
                variant="outline"
                className="font-mono text-[10px]"
              >
                {a.userId} → {a.roleId} @ {a.scope}
              </Badge>
            ))}
          </div>
        </div>
      )}

      <div>
        <div className="text-xs font-medium text-muted-foreground mb-1">
          Reasoning
        </div>
        <ol className="space-y-1.5">
          {result.reasoning.map((step, i) => (
            <li key={i} className="flex gap-2 text-sm">
              <span className="shrink-0 inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-muted text-muted-foreground text-[10px] font-mono px-1">
                {i + 1}
              </span>
              <div className="min-w-0">
                <div className="font-medium">{step.step}</div>
                {step.detail && (
                  <div className="text-xs text-muted-foreground font-mono break-words">
                    {step.detail}
                  </div>
                )}
              </div>
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}
