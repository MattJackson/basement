// SetRetentionDialog re-seed coverage (audit fe2).
//
// The dialog is mounted per VersionRow (open=false initially) and reused
// across openings. The lazy useState initialisers only run on first mount,
// so before the fix, opening the dialog on a DIFFERENT row — or after the
// per-version retention query resolved — showed the previous/default
// values instead of THIS version's actual remaining days + mode. The fix
// re-seeds via an effect keyed on [open, existingDate, existingMode].
//
// Two versions carry different active retentions (10 vs 40 days out, and
// GOVERNANCE vs COMPLIANCE). Opening each version's dialog must surface
// that version's own values.

import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, afterEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ObjectVersionsPanel } from "@/shared/ui/ObjectVersionsPanel";

function makeQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ client, children }: { client: QueryClient; children: React.ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

const DAY = 24 * 60 * 60 * 1000;

function installFetch(retentions: Record<string, { mode: string; retainUntilDate: string }>) {
  globalThis.fetch = vi.fn(async (url: RequestInfo | URL) => {
    const s = String(url);
    if (s.includes("/versions") && !s.includes("/retention") && !s.includes("/legal-hold")) {
      return new Response(
        JSON.stringify({
          versions: [
            { versionId: "v1", key: "k1", size: 10, isLatest: true },
            { versionId: "v2", key: "k1", size: 10, isLatest: false },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }
    if (s.includes("/object-lock") && !s.includes("/o/")) {
      return new Response(JSON.stringify({ enabled: true, supported: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (s.includes("/retention")) {
      const versionId = new URL(s, "http://x").searchParams.get("versionId") ?? "";
      return new Response(JSON.stringify({ retention: retentions[versionId] ?? null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (s.includes("/legal-hold")) {
      return new Response(JSON.stringify({ on: false }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;
}

describe("SetRetentionDialog re-seeds per row on open (audit fe2)", () => {
  it("shows each version's own days + mode when its dialog opens", async () => {
    installFetch({
      v1: { mode: "GOVERNANCE", retainUntilDate: new Date(Date.now() + 10 * DAY).toISOString() },
      v2: { mode: "COMPLIANCE", retainUntilDate: new Date(Date.now() + 40 * DAY).toISOString() },
    });

    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectVersionsPanel regionId="r1" bucket="b1" objectKey="k1" onClose={() => {}} />
      </Wrapper>,
    );

    // Wait for the retention queries to resolve (Edit retention label shows
    // once retention is active).
    const v1Action = await screen.findByTestId("object-version-retention-action-v1");

    // Open v1's dialog → ~10 days, Governance.
    await userEvent.click(v1Action);
    const dialog1 = await screen.findByRole("dialog");
    const days1 = within(dialog1).getByTestId("set-retention-days") as HTMLInputElement;
    await waitFor(() => expect(Number(days1.value)).toBe(10));
    expect((within(dialog1).getByTestId("set-retention-mode") as HTMLSelectElement).value).toBe(
      "GOVERNANCE",
    );

    // Close it.
    await userEvent.click(within(dialog1).getByRole("button", { name: /Cancel/i }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    // Open v2's dialog → must re-seed to ~40 days, Compliance (NOT v1's 10).
    await userEvent.click(screen.getByTestId("object-version-retention-action-v2"));
    const dialog2 = await screen.findByRole("dialog");
    const days2 = within(dialog2).getByTestId("set-retention-days") as HTMLInputElement;
    await waitFor(() => expect(Number(days2.value)).toBe(40));
    expect((within(dialog2).getByTestId("set-retention-mode") as HTMLSelectElement).value).toBe(
      "COMPLIANCE",
    );
  });
});
