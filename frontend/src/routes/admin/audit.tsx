// v1.0.0c — /admin/audit page.
//
// The "who did what when" view for incident response. Three sections:
//
//   1. Filter bar — actor + action + resource + result + date range.
//      Default window is the last 24 hours (calendar-driven; the
//      backend parses bare YYYY-MM-DD as start-of-day UTC for `from`
//      and end-of-day UTC for `to`).
//   2. Events table — Time / Actor / Action / Resource / Result /
//      Detail. Time renders relative ("3m ago") with the absolute
//      ISO timestamp on hover. Action shows as a colored badge so
//      the eye can pick out "share:revoke" rows from a sea of reads.
//      Result is a success/failure pill, also color-coded.
//   3. Empty + truncated states — bottom-of-table banners that
//      tell the operator EXACTLY what they're looking at ("no events
//      match this filter" vs. "the limit cut the results; narrow the
//      window or raise the limit").
//
// Right-clicking a row "Copy as JSON" — for an incident report. Pure
// clipboard, no server round-trip. The browser context menu still
// fires by default but the operator can drop into a code editor and
// paste the dump from the action button.

import { useMemo, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
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
import { adminPage } from "@/shared/layout/adminPage";
import { useAudit, type AuditEvent, type AuditFilter } from "@/shared/api/queries";

export const Route = createFileRoute("/admin/audit")({
  component: adminPage(AuditPage),
});

// Default window: last 24h. Backend treats bare YYYY-MM-DD as a
// calendar day in UTC, which is what the operator typing dates into
// the picker expects ("show me everything from yesterday").
function defaultFrom(): string {
  const d = new Date(Date.now() - 24 * 60 * 60 * 1000);
  return d.toISOString().slice(0, 10);
}
function defaultTo(): string {
  const d = new Date();
  return d.toISOString().slice(0, 10);
}

// v1.4.0a: default page size dropped from 200 → 50, and Prev/Next
// pagination replaces the prior "load more / narrow your window"
// blunt instrument. The previous-cycle audit (v1.3.0f) flagged the
// unfiltered 200-row scroll as a usability wart; 50 fits a laptop
// screen + pagination chrome.
const DEFAULT_PAGE_SIZE = 50;

function AuditPage() {
  // Local filter state — applied on Search click so typing in the
  // text inputs doesn't fire a fetch per keystroke. v1.4.0a: limit
  // defaults to 50; offset is page-driven.
  const [draft, setDraft] = useState<AuditFilter>({
    from: defaultFrom(),
    to: defaultTo(),
    actor: "",
    action: "",
    resource: "",
    result: "",
    limit: DEFAULT_PAGE_SIZE,
  });
  const [filter, setFilter] = useState<AuditFilter>(draft);
  const [offset, setOffset] = useState(0);
  const effectiveFilter: AuditFilter = { ...filter, offset };
  const { data, isLoading, error, refetch } = useAudit(effectiveFilter);

  const onSearch = () => {
    // Reset to page 1 whenever the filter changes — preserving the
    // offset would scroll past the end of the new result set.
    setOffset(0);
    setFilter(draft);
  };
  const onReset = () => {
    const f: AuditFilter = {
      from: defaultFrom(),
      to: defaultTo(),
      actor: "",
      action: "",
      resource: "",
      result: "",
      limit: DEFAULT_PAGE_SIZE,
    };
    setDraft(f);
    setFilter(f);
    setOffset(0);
  };

  const pageSize = data?.limit ?? filter.limit ?? DEFAULT_PAGE_SIZE;
  const currentOffset = data?.offset ?? offset;
  const total = data?.total ?? 0;
  const events = data?.events ?? [];

  return (
    <TooltipProvider>
      <div className="space-y-6">
        <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
          <div>
            <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
              Audit log
            </h1>
            <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
              Every mutating action across basement — who did it, when,
              against which resource, and whether it succeeded. Filter the
              window below; rows newest-first.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => exportCSV(events)} disabled={events.length === 0}>
              Export CSV
            </Button>
            <Button variant="outline" onClick={() => refetch()}>
              Refresh
            </Button>
          </div>
        </header>

        <FilterBar
          draft={draft}
          setDraft={setDraft}
          onSearch={onSearch}
          onReset={onReset}
        />

        {isLoading ? (
          <div className="rounded-lg border bg-card p-12 text-center text-sm text-muted-foreground">
            Loading audit events...
          </div>
        ) : error ? (
          <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
            Failed to load audit log: {(error as Error).message}
          </div>
        ) : (
          <>
            <EventsTable
              events={events}
              total={total}
              truncated={data?.truncated ?? false}
            />
            <PaginationBar
              offset={currentOffset}
              limit={pageSize}
              total={total}
              returned={events.length}
              onPrev={() => setOffset(Math.max(0, currentOffset - pageSize))}
              onNext={() => setOffset(currentOffset + pageSize)}
            />
          </>
        )}
      </div>
    </TooltipProvider>
  );
}

// PaginationBar renders Prev / Next + "showing X-Y of Z". Disabled
// affordances on out-of-bounds clicks so the operator can't paginate
// past the result set. We compute the page number from offset+limit
// rather than store it in state — single source of truth is the
// offset the server echoed back.
function PaginationBar({
  offset,
  limit,
  total,
  returned,
  onPrev,
  onNext,
}: {
  offset: number;
  limit: number;
  total: number;
  returned: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  if (total === 0) return null;
  const start = returned === 0 ? 0 : offset + 1;
  const end = offset + returned;
  const hasPrev = offset > 0;
  const hasNext = end < total;
  const page = Math.floor(offset / Math.max(1, limit)) + 1;
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, limit)));
  return (
    <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 text-xs text-muted-foreground">
      <div data-testid="audit-pagination-summary">
        Showing {start.toLocaleString()}-{end.toLocaleString()} of {total.toLocaleString()}
      </div>
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" disabled={!hasPrev} onClick={onPrev}>
          Previous
        </Button>
        <span className="tabular-nums">
          Page {page} of {totalPages}
        </span>
        <Button variant="outline" size="sm" disabled={!hasNext} onClick={onNext}>
          Next
        </Button>
      </div>
    </div>
  );
}

// exportCSV serialises the currently-visible page of events to a CSV
// download. Operator triggers this from the toolbar; we keep it
// client-side (no server round-trip) so the file always matches what
// they can see on screen — including the active filter and the
// current page. Multi-page CSV is a v1.4.0b ask.
function exportCSV(events: AuditEvent[]) {
  const header = ["time", "actor", "actorRole", "action", "resource", "result", "detail", "ip", "userAgent"];
  const escape = (v: unknown): string => {
    if (v === undefined || v === null) return "";
    const s = String(v);
    if (/[",\n]/.test(s)) {
      return `"${s.replace(/"/g, '""')}"`;
    }
    return s;
  };
  const rows = events.map((e) =>
    header.map((h) => escape((e as unknown as Record<string, unknown>)[h])).join(","),
  );
  const csv = [header.join(","), ...rows].join("\n");
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  // Audit-YYYY-MM-DDTHH-MM-SS.csv — readable in a directory listing,
  // collision-free across multiple exports in one session.
  const ts = new Date().toISOString().replace(/[:.]/g, "-");
  a.download = `audit-${ts}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function FilterBar({
  draft,
  setDraft,
  onSearch,
  onReset,
}: {
  draft: AuditFilter;
  setDraft: (f: AuditFilter) => void;
  onSearch: () => void;
  onReset: () => void;
}) {
  return (
    <section className="rounded-lg border bg-card p-4 space-y-3">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <Field label="Actor (exact)">
          <Input
            value={draft.actor ?? ""}
            placeholder="matthew"
            onChange={(e) => setDraft({ ...draft, actor: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && onSearch()}
          />
        </Field>
        <Field label="Action (substring)">
          <Input
            value={draft.action ?? ""}
            placeholder="bucket:delete"
            onChange={(e) => setDraft({ ...draft, action: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && onSearch()}
          />
        </Field>
        <Field label="Resource (substring)">
          <Input
            value={draft.resource ?? ""}
            placeholder="cluster:abc"
            onChange={(e) => setDraft({ ...draft, resource: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && onSearch()}
          />
        </Field>
        <Field label="Result">
          <select
            value={draft.result ?? ""}
            onChange={(e) => setDraft({ ...draft, result: e.target.value })}
            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="">Any</option>
            <option value="success">Success</option>
            <option value="failure">Failure</option>
          </select>
        </Field>
        <Field label="From (UTC)">
          <Input
            type="date"
            value={draft.from ?? ""}
            onChange={(e) => setDraft({ ...draft, from: e.target.value })}
          />
        </Field>
        <Field label="To (UTC)">
          <Input
            type="date"
            value={draft.to ?? ""}
            onChange={(e) => setDraft({ ...draft, to: e.target.value })}
          />
        </Field>
        <Field label="Page size">
          <Input
            type="number"
            min={1}
            max={1000}
            value={draft.limit ?? DEFAULT_PAGE_SIZE}
            onChange={(e) =>
              setDraft({ ...draft, limit: Number(e.target.value) || DEFAULT_PAGE_SIZE })
            }
          />
        </Field>
      </div>
      <div className="flex items-center justify-end gap-2 pt-1">
        <Button variant="ghost" onClick={onReset}>
          Reset
        </Button>
        <Button onClick={onSearch}>Search</Button>
      </div>
    </section>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="space-y-1 text-sm">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
  );
}

function EventsTable({
  events,
  total,
  truncated,
}: {
  events: AuditEvent[];
  total: number;
  truncated: boolean;
}) {
  if (events.length === 0) {
    return (
      <div className="rounded-lg border bg-muted/30 p-12 text-sm text-muted-foreground text-center">
        No events match the filter. Widen the date range or clear the search
        fields, then try again.
      </div>
    );
  }

  // v1.4.0a: total + truncated footer moved into PaginationBar; the
  // EventsTable now only carries the table itself. Keeping the props
  // signature stable so future cycles can re-introduce per-table
  // summary chrome without refactoring callers.
  void total;
  void truncated;
  return (
    <section className="space-y-2">
      <div className="rounded-lg border bg-card overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[180px]">Time</TableHead>
              <TableHead className="w-[140px]">Actor</TableHead>
              <TableHead className="w-[200px]">Action</TableHead>
              <TableHead>Resource</TableHead>
              <TableHead className="w-[100px]">Result</TableHead>
              <TableHead className="w-[40px] text-right"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((e, i) => (
              <EventRow key={i} event={e} />
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

function EventRow({ event }: { event: AuditEvent }) {
  const copyAsJson = () => {
    navigator.clipboard
      .writeText(JSON.stringify(event, null, 2))
      .then(() => toast.success("Event copied as JSON"))
      .catch(() => toast.error("Copy failed"));
  };

  const detail = event.detail ?? "";
  const hasDetail = detail !== "";

  return (
    <TableRow>
      <TableCell className="font-mono tabular-nums text-xs">
        <Tooltip>
          <TooltipTrigger asChild>
            <span>{relativeTime(event.time)}</span>
          </TooltipTrigger>
          <TooltipContent>{event.time}</TooltipContent>
        </Tooltip>
      </TableCell>
      <TableCell className="font-medium">
        {event.actor || <span className="text-muted-foreground">&mdash;</span>}
      </TableCell>
      <TableCell>
        <ActionBadge action={event.action} />
      </TableCell>
      <TableCell className="font-mono text-xs">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="block truncate max-w-md">{event.resource}</span>
          </TooltipTrigger>
          <TooltipContent>
            <div className="space-y-1 max-w-md">
              <div>
                <strong>Resource:</strong> {event.resource}
              </div>
              {hasDetail && (
                <div>
                  <strong>Detail:</strong> {detail}
                </div>
              )}
              {event.ip && (
                <div>
                  <strong>IP:</strong> {event.ip}
                </div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>
      </TableCell>
      <TableCell>
        <ResultPill result={event.result} />
      </TableCell>
      <TableCell className="text-right">
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={copyAsJson}
              className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors text-xs"
              title="Copy as JSON"
            >
              {"{}"}
            </button>
          </TooltipTrigger>
          <TooltipContent>Copy event as JSON</TooltipContent>
        </Tooltip>
      </TableCell>
    </TableRow>
  );
}

function ActionBadge({ action }: { action: string }) {
  const domain = action.split(":")[0] ?? "other";
  const color = useMemo(() => actionColor(domain), [domain]);
  return (
    <span
      className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium tabular-nums ${color}`}
    >
      {action}
    </span>
  );
}

function actionColor(domain: string): string {
  switch (domain) {
    case "cluster":
      return "bg-blue-500/10 text-blue-700 dark:text-blue-300";
    case "bucket":
      return "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "key":
      return "bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "share":
      return "bg-violet-500/10 text-violet-700 dark:text-violet-300";
    case "user":
      return "bg-cyan-500/10 text-cyan-700 dark:text-cyan-300";
    case "policy":
      return "bg-rose-500/10 text-rose-700 dark:text-rose-300";
    case "sync":
      return "bg-teal-500/10 text-teal-700 dark:text-teal-300";
    case "auth":
      return "bg-orange-500/10 text-orange-700 dark:text-orange-300";
    case "host":
      return "bg-slate-500/10 text-slate-700 dark:text-slate-300";
    default:
      return "bg-muted text-muted-foreground";
  }
}

function ResultPill({ result }: { result: string }) {
  if (result === "success") {
    return (
      <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-400">
        {"✓"} success
      </span>
    );
  }
  if (result === "failure") {
    return (
      <span className="inline-flex items-center rounded-full bg-rose-500/10 px-2 py-0.5 text-xs font-medium text-rose-700 dark:text-rose-400">
        {"✗"} failure
      </span>
    );
  }
  return <span className="text-xs text-muted-foreground">{result}</span>;
}

// relativeTime renders an ISO timestamp as "3m ago" / "2h ago" /
// "yesterday" / "Apr 14" — best-effort, single-line, English. The
// absolute ISO stays available on hover for the moments when the
// operator needs the exact value (incident timeline writeups).
function relativeTime(iso: string): string {
  const t = new Date(iso).getTime();
  if (isNaN(t)) return iso;
  const diffSec = Math.floor((Date.now() - t) / 1000);
  if (diffSec < 5) return "just now";
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay === 1) return "yesterday";
  if (diffDay < 7) return `${diffDay}d ago`;
  // Older than a week — show the date.
  return new Date(t).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}
