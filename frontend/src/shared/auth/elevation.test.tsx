// E2E-ish (vitest with mocked fetch) tests for the elevation guard
// orchestration (ADR-0003 v1.2.0b).
//
// Three scenarios that mirror what the operator experiences:
//
//   1. A wrapped mutation throws ELEVATION_REQUIRED → modal opens.
//   2. After a successful password submit, the original mutation is
//      retried automatically and returns the success value.
//   3. If the operator cancels the modal, the wrapped mutation
//      rejects with ELEVATION_CANCELLED and the caller sees the
//      original failure surfaced.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { useRef } from "react";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AuthModeProvider } from "@/shared/auth/mode";
import {
  ElevationProvider,
  useElevationGuard,
  isElevationRequired,
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

function Tree({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

// Test harness: a button that invokes runWithElevation against a
// scripted operation. The op's behavior is controlled via the
// `attempts` array passed in — each click consumes one entry. This
// lets the test stage "fail-then-succeed" for the retry case.
function GuardedButton({
  attempts,
  onResult,
}: {
  attempts: Array<"403" | "ok">;
  onResult: (r: { ok: boolean; value?: string; err?: unknown }) => void;
}) {
  const run = useElevationGuard();
  // useRef holds the cursor across the 403→retry cycle without
  // re-rendering when it advances (refs are explicitly allowed for
  // this kind of imperative bookkeeping per React 19 docs).
  const cursorRef = useRef(0);

  const op = async (): Promise<string> => {
    const i = cursorRef.current;
    cursorRef.current = i + 1;
    const verdict = attempts[i];
    if (verdict === "403") {
      const err = new Error(
        "ELEVATION_REQUIRED: Re-authentication required",
      ) as Error & {
        code: string;
        status: number;
        details?: Record<string, unknown>;
      };
      err.code = "ELEVATION_REQUIRED";
      err.status = 403;
      err.details = { mode_required: "admin", current_mode: "user" };
      throw err;
    }
    return "deleted-ok";
  };

  return (
    <button
      data-testid="go"
      onClick={() => {
        run(op)
          .then((v) => onResult({ ok: true, value: v }))
          .catch((e) => onResult({ ok: false, err: e }));
      }}
    >
      Delete
    </button>
  );
}

describe("isElevationRequired", () => {
  it("matches our apiError shape", () => {
    const err = Object.assign(new Error("x"), {
      code: "ELEVATION_REQUIRED",
      status: 403,
    });
    expect(isElevationRequired(err)).toBe(true);
  });

  it("rejects other errors", () => {
    expect(isElevationRequired(new Error("plain"))).toBe(false);
    expect(isElevationRequired({ code: "FORBIDDEN" })).toBe(false);
    expect(isElevationRequired(null)).toBe(false);
  });
});

describe("useElevationGuard end-to-end", () => {
  beforeEach(() => {
    // The modal's elevate POST goes through window.fetch — stub it.
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

  it("opens the modal on ELEVATION_REQUIRED and retries the op on success", async () => {
    const results: Array<{ ok: boolean; value?: string; err?: unknown }> = [];
    render(
      <Tree>
        <GuardedButton
          attempts={["403", "ok"]}
          onResult={(r) => results.push(r)}
        />
      </Tree>,
    );

    fireEvent.click(screen.getByTestId("go"));

    // Modal mounted after the 403 propagates through runWithElevation.
    const passwordInput = await screen.findByTestId("elevation-password");
    expect(passwordInput).toBeInTheDocument();

    // Type + submit. fetch is stubbed to 200 so the modal closes and
    // the original op re-fires + succeeds with "deleted-ok".
    fireEvent.change(passwordInput, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    await waitFor(() => expect(results).toHaveLength(1));
    expect(results[0]).toEqual({ ok: true, value: "deleted-ok" });
  });

  it("rejects with ELEVATION_CANCELLED when the operator clicks Cancel", async () => {
    const results: Array<{ ok: boolean; value?: string; err?: unknown }> = [];
    render(
      <Tree>
        <GuardedButton
          attempts={["403"]}
          onResult={(r) => results.push(r)}
        />
      </Tree>,
    );

    fireEvent.click(screen.getByTestId("go"));
    await screen.findByTestId("elevation-password");

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(results).toHaveLength(1));
    const first = results[0]!;
    expect(first.ok).toBe(false);
    const err = first.err as Error;
    expect(err.message).toBe("ELEVATION_CANCELLED");
  });

  it("surfaces non-403 errors directly without opening the modal", async () => {
    const results: Array<{ ok: boolean; value?: string; err?: unknown }> = [];

    function PassThroughButton() {
      const run = useElevationGuard();
      return (
        <button
          data-testid="go"
          onClick={() => {
            const op = async () => {
              const err = Object.assign(new Error("FORBIDDEN: nope"), {
                code: "FORBIDDEN",
                status: 403,
              });
              throw err;
            };
            run(op)
              .then((v) => results.push({ ok: true, value: v as string }))
              .catch((e) => results.push({ ok: false, err: e }));
          }}
        >
          Go
        </button>
      );
    }

    render(
      <Tree>
        <PassThroughButton />
      </Tree>,
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("go"));
    });

    expect(screen.queryByTestId("elevation-password")).not.toBeInTheDocument();
    expect(results).toHaveLength(1);
    const first = results[0]!;
    expect(first.ok).toBe(false);
    expect((first.err as Error).message).toMatch(/forbidden/i);
  });
});
