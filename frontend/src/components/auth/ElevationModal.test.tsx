// Tests for the elevation password modal (ADR-0003 v1.2.0b).
//
// Surfaces covered:
//   - Renders when `open` is true with the right title for each
//     target_mode.
//   - Submitting fires POST /auth/elevate with {password, target_mode},
//     updates the shared mode context, and calls onSuccess.
//   - 401 INVALID_PASSWORD shows an inline error and keeps the modal
//     open (the operator gets to try again without losing state).
//   - Cancel button fires onCancel and does not touch the network.
//
// We mock the global fetch instead of openapi-fetch because the modal
// uses bare fetch (the wire shape is hand-typed against ADR-0003).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ElevationModal } from "./ElevationModal";
import { AuthModeProvider, useAuthMode } from "@/shared/auth/mode";

// Sonner's toast spawns DOM nodes outside the modal; mock it to no-ops
// so we don't pollute screen queries.
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

function Harness(props: {
  open: boolean;
  targetMode: "admin" | "elevated";
  onSuccess: () => void;
  onCancel: () => void;
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider>
        <ModeProbe />
        <ElevationModal {...props} />
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

function ModeProbe() {
  const { mode, expiresAt } = useAuthMode();
  return (
    <span data-testid="mode-probe">
      {mode}|{expiresAt}
    </span>
  );
}

describe("ElevationModal", () => {
  // Keep the mocks under their own names so assertions like
  // `expect(onSuccessMock).toHaveBeenCalled()` keep working; expose
  // plain wrappers for the props (the Harness's `() => void` slot
  // doesn't accept vitest's dual-callable Mock type directly).
  let onSuccessMock = vi.fn<() => void>();
  let onCancelMock = vi.fn<() => void>();
  const onSuccess = (): void => {
    onSuccessMock();
  };
  const onCancel = (): void => {
    onCancelMock();
  };

  beforeEach(() => {
    onSuccessMock = vi.fn<() => void>();
    onCancelMock = vi.fn<() => void>();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders nothing when open is false", () => {
    render(
      <Harness
        open={false}
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("renders the admin title when targetMode is admin", () => {
    render(
      <Harness
        open
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );
    expect(screen.getByText(/elevate to admin mode/i)).toBeInTheDocument();
  });

  it("renders the elevated title when targetMode is elevated", () => {
    render(
      <Harness
        open
        targetMode="elevated"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );
    expect(
      screen.getByText(/re-enter password to continue/i),
    ).toBeInTheDocument();
  });

  it("submitting POSTs to /auth/elevate, updates mode state, and calls onSuccess", async () => {
    const expiresAtSec = Math.floor(Date.now() / 1000) + 900;
    const fetchSpy = vi
      .spyOn(window, "fetch")
      .mockImplementation(async (input, init) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.endsWith("/auth/elevate")) {
          // Sanity-check the request body shape.
          const body = JSON.parse(String(init?.body ?? "{}"));
          expect(body).toEqual({
            password: "hunter2",
            target_mode: "admin",
          });
          return new Response(
            JSON.stringify({
              mode: "admin",
              mode_expires_at: expiresAtSec,
              mode_ttl_seconds: 900,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response("{}", { status: 200 });
      });

    render(
      <Harness
        open
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );

    fireEvent.change(screen.getByTestId("elevation-password"), {
      target: { value: "hunter2" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    await waitFor(() => expect(onSuccessMock).toHaveBeenCalledTimes(1));
    expect(fetchSpy).toHaveBeenCalled();

    // Mode state should reflect admin + ms-converted expiry.
    expect(screen.getByTestId("mode-probe").textContent).toBe(
      `admin|${expiresAtSec * 1000}`,
    );
  });

  it("shows inline error and keeps modal open on 401 INVALID_PASSWORD", async () => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "INVALID_PASSWORD", message: "Invalid password" },
        }),
        { status: 401, headers: { "Content-Type": "application/json" } },
      ),
    );

    render(
      <Harness
        open
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );

    fireEvent.change(screen.getByTestId("elevation-password"), {
      target: { value: "wrong" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/invalid password/i);
    expect(onSuccessMock).not.toHaveBeenCalled();
    expect(onCancelMock).not.toHaveBeenCalled();
    // Modal still mounted.
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("Cancel button fires onCancel and does not call the network", () => {
    const fetchSpy = vi.spyOn(window, "fetch");
    render(
      <Harness
        open
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onCancelMock).toHaveBeenCalledTimes(1);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("rejects empty password without hitting the network", () => {
    const fetchSpy = vi.spyOn(window, "fetch");
    render(
      <Harness
        open
        targetMode="admin"
        onSuccess={onSuccess}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(/password/i);
  });
});
