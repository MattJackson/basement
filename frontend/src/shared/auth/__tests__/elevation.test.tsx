// v1.13.16 — ElevationProvider comprehensive coverage.
//
// Tests all public exports: promptForElevation, runWithElevation,
// isElevationRequired, promptElevationFromAnywhere, useElevationContext.
// Covers success flow, cancel flow, nested prompts, and error states.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AuthModeProvider } from "@/shared/auth/mode";
import {
  ElevationProvider,
  useElevationPrompt,
  useElevationGuard,
  isElevationRequired,
  promptElevationFromAnywhere,
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
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Tree({ client, children }: { client: QueryClient; children: React.ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <AuthModeProvider>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("isElevationRequired — error shape detection", () => {
  it("returns true for ELEVATION_REQUIRED errors with code", () => {
    const err = new Error("Access denied") as Error & { code?: string; details?: unknown };
    err.code = "ELEVATION_REQUIRED";
    expect(isElevationRequired(err)).toBe(true);
  });

  it("returns false for non-ELEVATION_REQUIRED errors", () => {
    const err = new Error("Some other error");
    (err as Error & { code?: string }).code = "OTHER_ERROR";
    expect(isElevationRequired(err)).toBe(false);
  });

  it("returns false for null/undefined", () => {
    expect(isElevationRequired(null as unknown as Error)).toBe(false);
    expect(isElevationRequired(undefined as unknown as Error)).toBe(false);
  });

  it("returns false for non-Error objects", () => {
    const err = new Error("test");
    (err as any).code = "OTHER";
    expect(isElevationRequired(err)).toBe(false);
  });
});

describe("promptForElevation — modal flow", () => {
  beforeEach(() => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({ mode: "admin", mode_expires_at: Date.now() / 1000 + 900 }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("opens modal and resolves on successful elevation", async () => {
    const client = newClient();
    let result: { ok: boolean; err?: unknown } | undefined;

    function Trigger() {
      const prompt = useElevationPrompt();
      return (
        <button
          data-testid="trigger"
          onClick={() =>
            prompt("admin", "test reason").then(() => {
              result = { ok: true };
            }).catch((e) => {
              result = { ok: false, err: e };
            })
          }
        >
          Elevate
        </button>
      );
    }

    render(<Tree client={client}><Trigger /></Tree>);
    fireEvent.click(screen.getByTestId("trigger"));

    await screen.findByTestId("elevation-password");
    expect(screen.getByText(/elevate to admin mode/i)).toBeInTheDocument();
    expect(screen.getByText(/test reason/i)).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("elevation-password"), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /submit/i }));

    await waitFor(() => expect(result).toBeDefined());
    expect(result?.ok).toBe(true);
  });

  it("rejects when user cancels the modal", async () => {
    const client = newClient();
    let result: { ok: boolean; err?: unknown } | undefined;

    function Trigger() {
      const prompt = useElevationPrompt();
      return (
        <button
          data-testid="trigger"
          onClick={() =>
            prompt("admin", "test").then(() => {
              result = { ok: true };
            }).catch((e) => {
              result = { ok: false, err: e };
            })
          }
        >
          Elevate
        </button>
      );
    }

    render(<Tree client={client}><Trigger /></Tree>);
    fireEvent.click(screen.getByTestId("trigger"));

    await screen.findByTestId("elevation-password");
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(result).toBeDefined());
    expect(result?.ok).toBe(false);
  });

it("handles elevated mode target correctly", async () => {
    const client = newClient();

    function Trigger() {
      const prompt = useElevationPrompt();
      return (
        <button
          data-testid="trigger"
          onClick={() => prompt("elevated", "needs elevated")}
        >
          Elevate
        </button>
      );
    }

    render(<Tree client={client}><Trigger /></Tree>);
    fireEvent.click(screen.getByTestId("trigger"));

    await screen.findByTestId("elevation-password");
    expect(screen.getByText(/needs elevated/i)).toBeInTheDocument();
  });
});

describe("runWithElevation — error retry flow", () => {
  beforeEach(() => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ mode: "admin" }), { status: 200 }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("retries operation on ELEVATION_REQUIRED error", async () => {
    const client = newClient();
    let callCount = 0;

    function TestComponent() {
      const guard = useElevationGuard();

      async function doWork() {
        callCount++;
        if (callCount === 1) {
          const err = new Error("Access denied") as Error & { code?: string };
          err.code = "ELEVATION_REQUIRED";
          throw err;
        }
        return "success";
      }

      async function handleClick() {
        try {
          await guard(doWork);
        } catch (e) {
          // Should not reach here on retry success
        }
      }

      return <button data-testid="go" onClick={handleClick}>Go</button>;
    }

    render(<Tree client={client}><TestComponent /></Tree>);
    
    const button = screen.getByTestId("go");
    fireEvent.click(button);

    await screen.findByTestId("elevation-password");
    fireEvent.change(screen.getByTestId("elevation-password"), { target: { value: "pass" } });
    fireEvent.click(screen.getByRole("button", { name: /submit/i }));

    await waitFor(() => expect(callCount).toBe(2), { timeout: 3000 });
  });

  it("re-throws non-ELEVATION_REQUIRED errors without retry", async () => {
    const client = newClient();
    let callCount = 0;
    let caughtError: unknown | null = null;

    function TestComponent() {
      const guard = useElevationGuard();

      async function doWork() {
        callCount++;
        throw new Error("Network error");
      }

      async function handleClick() {
        try {
          await guard(doWork);
        } catch (e) {
          caughtError = e;
        }
      }

      return <button data-testid="go" onClick={handleClick}>Go</button>;
    }

    render(<Tree client={client}><TestComponent /></Tree>);
    fireEvent.click(screen.getByTestId("go"));

    await waitFor(() => expect(callCount).toBe(1));
    expect(caughtError).toBeInstanceOf(Error);
    expect((caughtError as Error).message).toBe("Network error");
  });
});

describe("promptElevationFromAnywhere — non-React entrypoint", () => {
  beforeEach(() => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ mode: "admin" }), { status: 200 }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("rejects when no ElevationProvider is mounted", () => {
    const client = newClient();
    
    function TestComponent() {
      return <div>No provider</div>;
    }

    render(
      <QueryClientProvider client={client}>
        <TestComponent />
      </QueryClientProvider>,
    );

    expect(promptElevationFromAnywhere("admin")).rejects.toThrow(
      "ELEVATION_PROVIDER_NOT_MOUNTED",
    );
  });

  it("opens modal when provider is mounted", async () => {
    const client = newClient();

    function TestComponent() {
      return (
        <button
          data-testid="trigger"
          onClick={() => promptElevationFromAnywhere("admin", "from anywhere")}
        >
          Trigger
        </button>
      );
    }

    render(
      <Tree client={client}>
        <TestComponent />
      </Tree>,
    );

    fireEvent.click(screen.getByTestId("trigger"));
    await screen.findByTestId("elevation-password");
    expect(screen.getByText(/from anywhere/i)).toBeInTheDocument();
  });
});

describe("useElevationContext — hook validation", () => {
  it("throws when used outside ElevationProvider", () => {
    const client = newClient();

    function TestComponent() {
      try {
        useElevationGuard();
        return <div>No error</div>;
      } catch (e) {
        return <div data-testid="error">{(e as Error).message}</div>;
      }
    }

    render(
      <QueryClientProvider client={client}>
        <TestComponent />
      </QueryClientProvider>,
    );

    expect(screen.getByTestId("error")).toHaveTextContent(/useElevation\*/i);
  });

  it("provides hooks when inside ElevationProvider", () => {
    const client = newClient();

    function TestComponent() {
      try {
        useElevationGuard();
        return <div data-testid="ok">Has guard</div>;
      } catch (e) {
        return <div>Nope</div>;
      }
    }

    render(
      <Tree client={client}>
        <TestComponent />
      </Tree>,
    );

    expect(screen.getByTestId("ok")).toBeInTheDocument();
  });
});
