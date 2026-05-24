// v1.13.16 — ProtectedRoute route guard coverage.
//
// Guards /files and /admin/* routes by redirecting unauthenticated users
// to /login with a ?next= parameter preserving the intended destination.
// Prevents recursive redirects when already on /login.

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { ProtectedRoute } from "@/shared/auth/ProtectedRoute";

const navigateMock = vi.fn();
let mockedPathname = "/files";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal() as any;
  return {
    ...actual,
    useNavigate: () => navigateMock,
    useLocation: () => ({ pathname: mockedPathname }),
  };
});

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
