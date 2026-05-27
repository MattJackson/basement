// v1.13.16 — ProtectedRoute route guard coverage.
//
// Guards /files and /admin/* routes by redirecting unauthenticated users
// to /login with a ?next= parameter preserving the intended destination.
// Prevents recursive redirects when already on /login.

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";

const navigateMock = vi.fn();
const { toastInfoMock } = vi.hoisted(() => ({ toastInfoMock: vi.fn() }));
let mockedPathname = "/files";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal() as any;
  return {
    ...actual,
    useNavigate: () => navigateMock,
    useLocation: () => ({ pathname: mockedPathname }),
  };
});

vi.mock("sonner", () => ({
  toast: { info: toastInfoMock },
}));

let mockUserState: { data?: unknown; isLoading: boolean; isError: boolean } = {
  data: undefined,
  isLoading: true,
  isError: false,
};

vi.mock("@/shared/auth/useUser", () => ({
  useUser: vi.fn(() => mockUserState),
}));

describe("ProtectedRoute — authentication guards", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    navigateMock.mockClear();
    mockedPathname = "/files";
    mockUserState = { data: null, isLoading: true, isError: false };
  });

  it("renders LoadingSpinner while user data is loading", async () => {
    render(<ProtectedRoute><div>Child</div></ProtectedRoute>);
    
    expect(screen.getByTestId("loading-spinner")).toBeInTheDocument();
  });

  it("redirects unauthenticated users from /files to /login with next=/files", async () => {
    mockUserState = { data: undefined, isLoading: false, isError: false };

    render(<ProtectedRoute><div>Child</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          to: "/login",
          search: expect.objectContaining({ next: "/files" }),
        })
      );
    });
  });

  it("redirects unauthenticated users from /admin/clusters to /login with next=/admin/clusters", async () => {
    mockUserState = { data: undefined, isLoading: false, isError: false };
    mockedPathname = "/admin/clusters";

    render(<ProtectedRoute><div>Child</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          to: "/login",
          search: expect.objectContaining({ next: "/admin/clusters" }),
        })
      );
    });
  });

it("does not redirect when user is authenticated with /files path", async () => {
    mockUserState = { 
      data: { username: "alice" as string, role: "user" as const, uiAdmin: false, oidcUser: false },
      isLoading: false,
      isError: false,
    };

    render(<ProtectedRoute><div>Child</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).not.toHaveBeenCalled();
    });
  });

  it("does not redirect when already on /login", async () => {
    // v1.13.32: this test was previously passing for the wrong reason —
    // the redirect path was unreachable (guarded behind `!data` early
    // return) so navigateMock was never called for ANY unauth case,
    // making the assertion vacuously true. After fixing the unreachable
    // guard, this test must actually be on /login to validate the
    // "don't recurse" check.
    mockedPathname = "/login";
    mockUserState = { data: undefined, isLoading: false, isError: false };

    render(<ProtectedRoute><div>Child</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).not.toHaveBeenCalled();
    });
  });

  it("renders children when authenticated", async () => {
    mockUserState = {
      data: { username: "alice" as string, role: "user" as const, uiAdmin: false, oidcUser: false },
      isLoading: false,
      isError: false,
    };

    render(<ProtectedRoute><div data-testid="protected-content">Protected content</div></ProtectedRoute>);

    expect(screen.getByTestId("protected-content")).toBeInTheDocument();
  });
});

describe("ProtectedRoute — activeRole gating for /admin/clusters/*", () => {
  const uiAdmin = {
    username: "matthew",
    role: "admin" as const,
    uiAdmin: true,
    oidcUser: false,
    activeRole: { kind: "ui-admin" as const },
  };
  const clusterAdminA = {
    username: "matthew",
    role: "admin" as const,
    uiAdmin: true,
    oidcUser: false,
    activeRole: { kind: "cluster-admin" as const, cluster: "cluster-a" },
  };
  const plainUser = {
    username: "alice",
    role: "user" as const,
    uiAdmin: false,
    oidcUser: false,
    activeRole: { kind: "user" as const },
  };

  beforeEach(() => {
    vi.clearAllMocks();
    navigateMock.mockClear();
    toastInfoMock.mockClear();
  });

  it("UI Admin can reach /admin/clusters/new (was blocked before B2 fix)", async () => {
    mockedPathname = "/admin/clusters/new";
    mockUserState = { data: uiAdmin, isLoading: false, isError: false };

    render(<ProtectedRoute><div data-testid="ok">ok</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).not.toHaveBeenCalled();
      expect(toastInfoMock).not.toHaveBeenCalled();
    });
  });

  it("UI Admin can reach /admin/clusters/{cid}/edit on ANY cluster", async () => {
    mockedPathname = "/admin/clusters/cluster-z/edit";
    mockUserState = { data: uiAdmin, isLoading: false, isError: false };

    render(<ProtectedRoute><div data-testid="ok">ok</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).not.toHaveBeenCalled();
    });
  });

  it("Cluster Admin can reach /admin/clusters/{their-cluster}/...", async () => {
    mockedPathname = "/admin/clusters/cluster-a/edit";
    mockUserState = { data: clusterAdminA, isLoading: false, isError: false };

    render(<ProtectedRoute><div data-testid="ok">ok</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).not.toHaveBeenCalled();
    });
  });

  it("Cluster Admin hitting a DIFFERENT cluster is redirected to their own (silent)", async () => {
    mockedPathname = "/admin/clusters/cluster-b/edit";
    mockUserState = { data: clusterAdminA, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters/cluster-a" });
      expect(toastInfoMock).not.toHaveBeenCalled();
    });
  });

  it("Cluster Admin cannot reach /admin/clusters/new (UI-Admin-only) and gets toast", async () => {
    mockedPathname = "/admin/clusters/new";
    mockUserState = { data: clusterAdminA, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
      expect(toastInfoMock).toHaveBeenCalledWith(
        expect.stringContaining("UI Admin"),
      );
    });
  });

  it("Plain user is bounced from /admin/clusters/new with admin-role toast", async () => {
    mockedPathname = "/admin/clusters/new";
    mockUserState = { data: plainUser, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
      expect(toastInfoMock).toHaveBeenCalled();
    });
  });

  it("Plain user is bounced from /admin/clusters/{cid}/edit with admin-role toast", async () => {
    mockedPathname = "/admin/clusters/cluster-a/edit";
    mockUserState = { data: plainUser, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
      expect(toastInfoMock).toHaveBeenCalledWith(
        expect.stringContaining("admin"),
      );
    });
  });

  it("Plain user is bounced from /admin/users with toast", async () => {
    mockedPathname = "/admin/users";
    mockUserState = { data: plainUser, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
      expect(toastInfoMock).toHaveBeenCalledWith(
        expect.stringContaining("UI Admin"),
      );
    });
  });
});

describe("ProtectedRoute — /admin landing (v2.0.0-beta.23)", () => {
  const uiAdmin = {
    username: "matthew",
    role: "admin" as const,
    uiAdmin: true,
    oidcUser: false,
    activeRole: { kind: "ui-admin" },
  };

  beforeEach(() => {
    vi.clearAllMocks();
    navigateMock.mockClear();
    mockedPathname = "/admin";
  });

  it("UI Admin landing on /admin is redirected to /admin/system", async () => {
    mockUserState = { data: uiAdmin, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/system" });
    });
  });

  it("Cluster Admin landing on /admin is redirected to their cluster", async () => {
    const clusterAdminA = {
      username: "matthew",
      role: "admin" as const,
      uiAdmin: true,
      oidcUser: false,
      activeRole: { kind: "cluster-admin", cluster: "lsi" },
    };
    
    mockUserState = { data: clusterAdminA, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters/lsi" });
    });
  });

  it("Plain user is bounced from /admin with toast", async () => {
    const plainUser = {
      username: "alice",
      role: "user" as const,
      uiAdmin: false,
      oidcUser: false,
      activeRole: { kind: "user" },
    };

    mockUserState = { data: plainUser, isLoading: false, isError: false };

    render(<ProtectedRoute><div>blocked</div></ProtectedRoute>);

    await vi.waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
      expect(toastInfoMock).toHaveBeenCalled();
    });
  });
});
