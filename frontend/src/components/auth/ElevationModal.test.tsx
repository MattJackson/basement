// Tests for the elevation modal (ADR-0003 v1.2.0b + v1.2.0c).
//
// Surfaces covered:
//   - Renders when `open` is true with the right title for each
//     target_mode.
//   - Local-password path (v1.2.0b):
//     - Submitting fires POST /auth/elevate with {password, target_mode},
//       updates the shared mode context, and calls onSuccess.
//     - 401 INVALID_PASSWORD shows an inline error and keeps the modal
//       open (the operator gets to try again without losing state).
//     - Cancel button fires onCancel and does not touch the network.
//   - OIDC path (v1.2.0c):
//     - When /auth/me reports oidcUser=true the modal renders the
//       "Elevate via SSO" button (no password field).
//     - Clicking SSO POSTs /auth/elevate/oidc/start and full-page-
//       navigates to the returned redirect_url.
//     - 503 OIDC_NOT_CONFIGURED renders the "contact your administrator"
//       error inline.
//   - Dispatcher pivot (v1.2.0c): local-password POST that comes back
//     with requires_oidc:true fans out into the OIDC start endpoint
//     and follows redirect_url — no setAuthMode + no onSuccess.
//
// We mock the global fetch instead of openapi-fetch because the modal
// uses bare fetch (the wire shape is hand-typed against ADR-0003).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ElevationModal } from "./ElevationModal";
import { AuthModeProvider, useAuthMode } from "@/shared/auth/mode";

// useUser drives the OIDC-vs-local branch in the modal. The hook
// internally calls openapi-fetch's client.GET("/auth/me"), which
// captures globalThis.fetch at module-import time — so a window.fetch
// spy installed in beforeEach can't intercept it. Mocking the hook
// directly keeps the test focused on the modal's branching behaviour
// instead of openapi-fetch internals.
vi.mock("@/shared/auth/useUser", () => ({
  useUser: vi.fn(() => ({
    data: undefined,
    isLoading: false,
    isError: false,
  })),
}));

import { useUser } from "@/shared/auth/useUser";
const useUserMock = vi.mocked(useUser);

function setLocalUser() {
  useUserMock.mockReturnValue({
    data: {
      username: "alice",
      role: "user",
      uiAdmin: false,
      mode: "user",
      modeExpiresAt: 0,
      oidcUser: false,
    } as unknown as ReturnType<typeof useUser>["data"],
    isLoading: false,
    isError: false,
  });
}

function setOIDCUser() {
  useUserMock.mockReturnValue({
    data: {
      username: "alice",
      role: "user",
      uiAdmin: false,
      mode: "user",
      modeExpiresAt: 0,
      oidcUser: true,
    } as unknown as ReturnType<typeof useUser>["data"],
    isLoading: false,
    isError: false,
  });
}

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
    // Default to a local-password user so the v1.2.0b tests below
    // exercise the password form (unchanged). OIDC tests opt in via
    // setOIDCUser().
    setLocalUser();
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

  // OIDC-only user path (v1.2.0c). The /auth/me payload has
  // oidcUser=true so the modal renders the SSO button instead of the
  // password form, and clicking it POSTs /auth/elevate/oidc/start +
  // navigates to the IdP.
  describe("OIDC variant (oidcUser=true)", () => {
    // Stub window.location.href so we can assert the redirect without
    // jsdom actually trying to navigate. jsdom's window.location is
    // strict — we replace just the href setter via Object.defineProperty.
    let lastHref = "";
    let originalDescriptor: PropertyDescriptor | undefined;
    beforeEach(() => {
      lastHref = "";
      originalDescriptor = Object.getOwnPropertyDescriptor(
        window,
        "location",
      );
      const stub: Pick<Location, "href"> & { assign: (u: string) => void } = {
        href: "",
        assign: (u: string) => {
          lastHref = u;
        },
      };
      Object.defineProperty(stub, "href", {
        configurable: true,
        get: () => lastHref,
        set: (u: string) => {
          lastHref = u;
        },
      });
      Object.defineProperty(window, "location", {
        configurable: true,
        value: stub,
      });
    });
    afterEach(() => {
      if (originalDescriptor) {
        Object.defineProperty(window, "location", originalDescriptor);
      }
    });

    beforeEach(() => {
      setOIDCUser();
    });

    function installStartFetch(
      handler: (init: RequestInit) => Response | Promise<Response>,
    ) {
      return vi.spyOn(window, "fetch").mockImplementation(async (input, init) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.endsWith("/auth/elevate/oidc/start")) {
          return handler(init ?? {});
        }
        return new Response("{}", { status: 200 });
      });
    }

    it("renders the SSO button (no password field) when user is OIDC-only", () => {
      render(
        <Harness
          open
          targetMode="admin"
          onSuccess={onSuccess}
          onCancel={onCancel}
        />,
      );
      expect(screen.getByTestId("elevation-oidc-submit")).toBeInTheDocument();
      expect(
        screen.queryByTestId("elevation-password"),
      ).not.toBeInTheDocument();
    });

    it("clicking Elevate via SSO POSTs /auth/elevate/oidc/start + navigates to redirect_url", async () => {
      const fetchSpy = installStartFetch(async (init) => {
        const body = JSON.parse(String(init.body ?? "{}"));
        expect(body).toEqual({ target_mode: "admin" });
        return new Response(
          JSON.stringify({
            redirect_url: "https://idp.example.com/auth?prompt=login",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      });

      render(
        <Harness
          open
          targetMode="admin"
          onSuccess={onSuccess}
          onCancel={onCancel}
        />,
      );

      fireEvent.click(screen.getByTestId("elevation-oidc-submit"));

      await waitFor(() => {
        expect(lastHref).toBe(
          "https://idp.example.com/auth?prompt=login",
        );
      });
      const startCalls = fetchSpy.mock.calls.filter(([u]) =>
        String(u).endsWith("/auth/elevate/oidc/start"),
      );
      expect(startCalls.length).toBe(1);
      // onSuccess is NOT called — the elevation hasn't happened yet;
      // it'll land via the callback's Set-Cookie + ?elevated= redirect.
      expect(onSuccessMock).not.toHaveBeenCalled();
    });

    it("503 OIDC_NOT_CONFIGURED renders the 'contact your administrator' message inline", async () => {
      installStartFetch(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "OIDC_NOT_CONFIGURED",
                message: "OIDC is not configured on this server.",
              },
            }),
            {
              status: 503,
              headers: { "Content-Type": "application/json" },
            },
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

      fireEvent.click(screen.getByTestId("elevation-oidc-submit"));

      const alert = await screen.findByRole("alert");
      expect(alert.textContent).toMatch(/contact your administrator/i);
      expect(lastHref).toBe("");
    });
  });

  // Dispatcher pivot (v1.2.0c): a local-password submit comes back
  // with requires_oidc:true — the modal then transparently kicks off
  // the OIDC start flow without surfacing an error. Pretend the user
  // is local (default beforeEach) so the password form renders, then
  // exercise the on-response pivot.
  it("dispatcher pivot: requires_oidc:true follows start_url + navigates", async () => {
    let lastHref = "";
    const originalDescriptor = Object.getOwnPropertyDescriptor(
      window,
      "location",
    );
    const stub: Pick<Location, "href"> = { href: "" };
    Object.defineProperty(stub, "href", {
      configurable: true,
      get: () => lastHref,
      set: (u: string) => {
        lastHref = u;
      },
    });
    Object.defineProperty(window, "location", {
      configurable: true,
      value: stub,
    });

    try {
      vi.spyOn(window, "fetch").mockImplementation(async (input, init) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.endsWith("/auth/elevate")) {
          return new Response(
            JSON.stringify({
              requires_oidc: true,
              start_url: "/api/v1/auth/elevate/oidc/start",
            }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          );
        }
        if (url.endsWith("/auth/elevate/oidc/start")) {
          const body = JSON.parse(String(init?.body ?? "{}"));
          expect(body).toEqual({ target_mode: "admin" });
          return new Response(
            JSON.stringify({ redirect_url: "https://idp.example.com/x" }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
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
        target: { value: "anything" },
      });
      fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

      await waitFor(() => {
        expect(lastHref).toBe("https://idp.example.com/x");
      });
      // No onSuccess — elevation completes server-side later.
      expect(onSuccessMock).not.toHaveBeenCalled();
    } finally {
      if (originalDescriptor) {
        Object.defineProperty(window, "location", originalDescriptor);
      }
    }
  });
});
