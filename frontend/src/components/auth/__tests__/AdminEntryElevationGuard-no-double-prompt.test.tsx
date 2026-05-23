// v1.9.0e.1 regression test — AdminEntryElevationGuard must NOT fire
// a second prompt after a successful elevation.
//
// Bug being pinned: matthew on /files clicks "Switch to admin" in
// UserMenu → password modal → elevation succeeds → UserMenu navigates
// to /admin/clusters → AppShell mounts the guard → guard reads the
// (stale) cached /auth/me payload that still says mode=user → fires
// the modal AGAIN. The v1.7.0a.1 guard checks `user.mode` directly off
// the cached payload to defer to the server's word; if the cache
// hasn't been invalidated post-success, the server's "word" is the
// pre-elevation value.
//
// The fix lives in ElevationProvider.handleSuccess (invalidate the
// ["auth", "me"] query). This test verifies the contract at the guard
// boundary: when the cached useUser payload reports mode=admin AFTER
// elevation success, the guard must stay silent.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AdminEntryElevationGuard } from "@/components/auth/AdminEntryElevationGuard";
import { AuthModeProvider, type AuthMode } from "@/shared/auth/mode";

// Router mocks — guard is on /admin/clusters for the duration.
let mockedPath = "/admin/clusters";
const navigateSpy = vi.fn((_opts: { to: string }) => Promise.resolve());
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: mockedPath }),
  useNavigate: () => navigateSpy,
}));

vi.mock("sonner", () => ({
  toast: {
    info: vi.fn(),
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  },
}));

const promptSpy = vi.fn((_target: AuthMode, _reason?: string) =>
  Promise.resolve(),
);
vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => promptSpy,
}));

// useUser response is controlled per test by mutating this object. The
// mock factory closes over `currentUser` so re-renders pick up the
// latest value without us having to re-mock the module.
let currentUser: {
  username: string;
  role: "admin" | "user";
  uiAdmin: boolean;
  oidcUser: boolean;
  mode?: AuthMode;
  modeExpiresAt?: number;
} | undefined;
let currentUserLoading = false;
vi.mock("@/shared/auth/useUser", () => ({
  useUser: () => ({
    data: currentUser,
    isLoading: currentUserLoading,
    isError: false,
  }),
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Harness({
  client,
  initial,
}: {
  client: QueryClient;
  initial?: { mode: AuthMode; expiresAt: number };
}) {
  return (
    <QueryClientProvider client={client}>
      <AuthModeProvider initial={initial}>
        <AdminEntryElevationGuard />
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("AdminEntryElevationGuard — no double prompt after elevation (v1.9.0e.1)", () => {
  beforeEach(() => {
    mockedPath = "/admin/clusters";
    promptSpy.mockClear();
    navigateSpy.mockClear();
    currentUserLoading = false;
  });

  it("Cached /auth/me reports mode=admin → guard stays silent", async () => {
    // Post-elevation, post-invalidate state: the React Query cache has
    // refetched and now reports the operator is in admin mode. The
    // AuthModeProvider's local state is still "user" because the
    // AuthModeHydrator hasn't yet propagated the new value (this is the
    // exact mid-flight render where the bug bit). The guard MUST defer
    // to user.mode === "admin" and skip the prompt.
    currentUser = {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      mode: "admin",
      modeExpiresAt: Math.floor(Date.now() / 1000) + 600,
    };
    render(<Harness client={newClient()} initial={{ mode: "user", expiresAt: 0 }} />);

    // Let any post-render effects run.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(promptSpy).not.toHaveBeenCalled();
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  it("Cached /auth/me reports mode=elevated → guard stays silent", async () => {
    // Same shape as above but with the legacy "elevated" alias —
    // verifies both server-reported elevated values short-circuit the
    // prompt.
    currentUser = {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      mode: "elevated",
      modeExpiresAt: Math.floor(Date.now() / 1000) + 600,
    };
    render(<Harness client={newClient()} initial={{ mode: "user", expiresAt: 0 }} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(promptSpy).not.toHaveBeenCalled();
  });

  it("Cached /auth/me reports mode=user (pre-elevation stale) → guard DOES prompt", async () => {
    // Sanity check: when the cache really does say user (no
    // invalidation happened, the bug scenario), the guard fires. This
    // confirms the silent path above is driven by the cached mode and
    // not by some unrelated short-circuit.
    currentUser = {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      mode: "user",
      modeExpiresAt: 0,
    };
    render(<Harness client={newClient()} initial={{ mode: "user", expiresAt: 0 }} />);

    await waitFor(() => {
      expect(promptSpy).toHaveBeenCalledTimes(1);
    });
  });

  it("Post-elevation refetch flips cached mode → no late prompt", async () => {
    // Two-phase: initially the cache is stale (mode=user); then the
    // /auth/me refetch lands (mode=admin) and we re-render. The guard
    // SHOULD have fired in phase 1 (that's the existing v1.7.0a.1
    // behaviour — auto-prompt on stale cache); the new check is that
    // once the cache flips, no NEW prompt fires for subsequent renders
    // even if the URL stays on /admin/*. Together with the post-
    // success invalidate, this means a real elevation cycle never
    // double-prompts: phase 1's prompt is the one the operator
    // submitted, phase 2 catches the success and stays quiet.
    currentUser = {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      mode: "user",
      modeExpiresAt: 0,
    };
    const stableClient = newClient();
    const { rerender } = render(
      <Harness client={stableClient} initial={{ mode: "user", expiresAt: 0 }} />,
    );
    await waitFor(() => {
      expect(promptSpy).toHaveBeenCalledTimes(1);
    });

    // Refetch landed — cache now says admin. Re-render with the same
    // QueryClient + AuthModeProvider mount so we test the within-mount
    // re-run behaviour (which is what the live cycle exercises).
    currentUser = {
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      mode: "admin",
      modeExpiresAt: Math.floor(Date.now() / 1000) + 600,
    };
    rerender(<Harness client={stableClient} initial={{ mode: "user", expiresAt: 0 }} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    // Still exactly one call — the post-refetch render did NOT add a
    // second prompt. (A second prompt here would be the operator-
    // reported double-prompt bug.)
    expect(promptSpy).toHaveBeenCalledTimes(1);
  });
});
