// Tests for /admin/first-run — the v1.11.0a onboarding wizard.
//
// Covers:
//   1. Welcome step renders + the "I'll set up later" button calls
//      /admin/onboarding/dismiss and navigates away.
//   2. The stepper advances through all five steps via Next/Skip and
//      mounts the right copy on each.
//   3. The Done-step "Finish" button also dismisses (so both Skip and
//      complete paths converge on the same latch).

import { describe, it, expect, vi, beforeEach } from "vitest";

const { navigateMock, fetchMock, useCreateClusterMock, clientMock } = vi.hoisted(() => ({
  navigateMock: vi.fn(),
  fetchMock: vi.fn(),
  useCreateClusterMock: vi.fn(),
  clientMock: { POST: vi.fn(), PATCH: vi.fn() },
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => () => ({}),
    useNavigate: () => navigateMock,
    Link: ({ children, ...rest }: { children: React.ReactNode } & Record<string, unknown>) => (
      <a {...rest}>{children}</a>
    ),
  };
});

vi.mock("@/shared/layout/adminPage", () => ({
  adminPage: (Page: React.ComponentType) => Page,
}));

vi.mock("@/shared/api/mutations", () => ({
  useCreateCluster: () => useCreateClusterMock(),
}));

vi.mock("@/shared/api/client", () => ({
  client: clientMock,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import FirstRunWizard from "@/routes/admin/first-run";

beforeEach(() => {
  vi.clearAllMocks();
  navigateMock.mockReset();
  fetchMock.mockReset();
  useCreateClusterMock.mockReset();
  clientMock.POST.mockReset();
  clientMock.PATCH.mockReset();

  // Always return a permissive cluster-create mutation so the wizard
  // doesn't crash trying to access .isPending on undefined.
  useCreateClusterMock.mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({ id: "conn-new" }),
    isPending: false,
    error: null,
  });

  // Global fetch — the dismiss path uses it; default to a 200 OK.
  vi.stubGlobal(
    "fetch",
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ needsOnboarding: true, completed: false }),
    }),
  );
});

describe("FirstRunWizard (/admin/first-run)", () => {
  it("renders the welcome step with both Get started and I'll set up later buttons", () => {
    render(<FirstRunWizard />);
    expect(screen.getByRole("heading", { name: /Welcome to basement/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Get started/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /I'll set up later/i })).toBeInTheDocument();
    // Stepper announces "Step 1 of 5"
    expect(screen.getByText(/Step 1 of 5/i)).toBeInTheDocument();
  });

  it("dismisses + navigates when the operator clicks I'll set up later", async () => {
    render(<FirstRunWizard />);
    await userEvent.click(screen.getByRole("button", { name: /I'll set up later/i }));
    // Dismiss endpoint was hit.
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/admin/onboarding/dismiss",
      expect.objectContaining({ method: "POST" }),
    );
    // Navigation kicked off.
    expect(navigateMock).toHaveBeenCalled();
  });

  it("advances through each step via Skip and reaches the Done card", async () => {
    render(<FirstRunWizard />);

    // Welcome -> Cluster
    await userEvent.click(screen.getByRole("button", { name: /Get started/i }));
    expect(screen.getByRole("heading", { name: /Add your first cluster/i })).toBeInTheDocument();
    expect(screen.getByText(/Step 2 of 5/i)).toBeInTheDocument();

    // Cluster -> OIDC (Skip — required fields not filled)
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));
    expect(screen.getByRole("heading", { name: /Configure single sign-on/i })).toBeInTheDocument();
    expect(screen.getByText(/Step 3 of 5/i)).toBeInTheDocument();

    // OIDC -> User
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));
    expect(screen.getByRole("heading", { name: /Invite a team member/i })).toBeInTheDocument();
    expect(screen.getByText(/Step 4 of 5/i)).toBeInTheDocument();

    // User -> Done
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));
    expect(screen.getByRole("heading", { name: /Setup complete/i })).toBeInTheDocument();
    expect(screen.getByText(/Step 5 of 5/i)).toBeInTheDocument();
  });

  it("Finish on the Done step calls dismiss + navigates", async () => {
    render(<FirstRunWizard />);

    // Skip all the way to Done.
    await userEvent.click(screen.getByRole("button", { name: /Get started/i }));
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));
    await userEvent.click(screen.getByRole("button", { name: /^Skip$/i }));

    // Click Finish on the Done card.
    const finishBtn = screen.getByRole("button", { name: /Finish/i });
    await userEvent.click(finishBtn);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/admin/onboarding/dismiss",
      expect.objectContaining({ method: "POST" }),
    );
    expect(navigateMock).toHaveBeenCalled();
  });
});
