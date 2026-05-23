// v1.9.0e.1 regression test — promptForElevation success must
// invalidate the cached /auth/me query so the AuthModeHydrator picks up
// the freshly-rotated cookie mode immediately.
//
// Bug being pinned: matthew on /files clicks "Switch to admin" in the
// UserMenu → password modal → password submits → cookie now has
// mode=admin, but React Query's cached /auth/me payload still says
// mode=user. AdminEntryElevationGuard reads `user.mode` directly off
// the cached payload, sees "user", and pops the modal again. Same shape
// as the v1.7.0a.2 drop-privileges cache-staleness bug, just on the
// rising edge.
//
// This test:
//   1. Stages a QueryClient with a known ["auth", "me"] cache entry.
//   2. Invokes promptForElevation programmatically.
//   3. Submits the modal (fetch /auth/elevate stubbed to 200).
//   4. Asserts the [auth, me] query was invalidated after success.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AuthModeProvider } from "@/shared/auth/mode";
import {
  ElevationProvider,
  useElevationPrompt,
} from "@/shared/auth/elevation";

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Tree({
  client,
  children,
}: {
  client: QueryClient;
  children: React.ReactNode;
}) {
  return (
    <QueryClientProvider client={client}>
      <AuthModeProvider>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

// Test trigger: a button that fires promptForElevation when clicked.
// Mirrors what UserMenu's "Switch to admin view" handler does.
function PromptTrigger({
  onResult,
}: {
  onResult: (r: { ok: boolean; err?: unknown }) => void;
}) {
  const prompt = useElevationPrompt();
  return (
    <button
      data-testid="go"
      onClick={() => {
        prompt("admin", "test reason")
          .then(() => onResult({ ok: true }))
          .catch((e) => onResult({ ok: false, err: e }));
      }}
    >
      Switch to admin
    </button>
  );
}

describe("ElevationProvider — handleSuccess cache invalidation (v1.9.0e.1)", () => {
  beforeEach(() => {
    // Stub the elevate POST — returns mode=admin with a fresh TTL.
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          mode: "admin",
          mode_expires_at: Math.floor(Date.now() / 1000) + 900,
          mode_ttl_seconds: 900,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("invalidates [auth, me] after a successful elevation", async () => {
    const client = newClient();
    // Seed a stale /auth/me payload so we can assert it was invalidated.
    client.setQueryData(["auth", "me"], {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      mode: "user",
      modeExpiresAt: 0,
      oidcUser: false,
    });
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");

    const results: Array<{ ok: boolean; err?: unknown }> = [];
    render(
      <Tree client={client}>
        <PromptTrigger onResult={(r) => results.push(r)} />
      </Tree>,
    );

    fireEvent.click(screen.getByTestId("go"));

    // Modal mounts after the prompt fires.
    const passwordInput = await screen.findByTestId("elevation-password");
    fireEvent.change(passwordInput, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    // Wait for the prompt to resolve.
    await waitFor(() => expect(results).toHaveLength(1));
    expect(results[0]?.ok).toBe(true);

    // Both [auth, me] and [user] are invalidated on success.
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["auth", "me"] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["user"] });
  });

  it("does NOT invalidate [auth, me] when the operator cancels the modal", async () => {
    const client = newClient();
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");

    const results: Array<{ ok: boolean; err?: unknown }> = [];
    render(
      <Tree client={client}>
        <PromptTrigger onResult={(r) => results.push(r)} />
      </Tree>,
    );

    fireEvent.click(screen.getByTestId("go"));
    await screen.findByTestId("elevation-password");

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(results).toHaveLength(1));
    expect(results[0]?.ok).toBe(false);

    // Cancellation must NOT invalidate the cache — nothing changed
    // server-side, no reason to refetch.
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: ["auth", "me"],
    });
  });
});
