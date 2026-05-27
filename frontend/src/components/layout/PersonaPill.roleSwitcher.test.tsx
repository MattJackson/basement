// v2.0.0-beta.24: role-switcher coverage moved to its own file so the
// vi.mock("@/shared/auth/useUser") that's needed to populate
// availableRoles doesn't leak into the warning-window + drop-privileges
// tests in PersonaPill.test.tsx. Vitest hoists vi.mock to module level,
// so a mock declared inside a describe's beforeEach still applies to
// every other describe in the same file. Keeping each scenario's mocks
// in its own file is the only reliable scope.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { PersonaPill, __resetPersonaPillTestState } from "./PersonaPill";
import { AuthModeProvider } from "@/shared/auth/mode";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
}));

vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => vi.fn(() => Promise.resolve()),
  promptElevationFromAnywhere: vi.fn(() => Promise.resolve()),
}));

const navigateSpy = vi.fn((_opts: { to: string }) => Promise.resolve());
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@/shared/auth/useUser", () => ({
  useUser: () => ({
    data: {
      username: "matthew",
      activeRole: { kind: "user" as const },
      availableRoles: [
        { kind: "user" as const, label: "User" },
        { kind: "ui-admin" as const, label: "UI Admin" },
      ],
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@/shared/api/mutations", () => ({
  useSwitchActiveRole: () => ({
    isPending: false,
    mutateAsync: vi.fn(() => Promise.resolve()),
  }),
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe("PersonaPill — role switcher (v2.0.0-beta.23)", () => {
  beforeEach(() => {
    __resetPersonaPillTestState();
    navigateSpy.mockClear();
  });

  it("renders the role trigger button when multiple roles are available", async () => {
    render(
      <QueryClientProvider client={newClient()}>
        <AuthModeProvider initial={{ mode: "user", expiresAt: 0 }}>
          <PersonaPill />
        </AuthModeProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByTestId("persona-role-trigger")).toBeInTheDocument();
  });
});
