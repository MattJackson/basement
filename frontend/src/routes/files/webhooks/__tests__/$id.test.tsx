// Tests for /files/webhooks/$id detail page (v1.7.0e).
//
// Asserts:
//   - Configuration card renders with the redacted secret status
//   - Last-delivery card surfaces success / status / duration
//   - Test webhook button calls the mutation
//   - Toggle button calls enable/disable depending on current state
//   - Delete uses window.confirm — accept = mutate
//   - Edit flow: opening the form, saving with a new secret triggers
//     the rotate path of the SecretShownOnceDialog

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      useParams: () => ({ id: "wh-1" }),
    }),
    Link: ({ children }: any) => <a>{children}</a>,
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useWebhook: vi.fn(),
    useDeleteWebhook: vi.fn(),
    useTestWebhook: vi.fn(),
    useEnableWebhook: vi.fn(),
    useDisableWebhook: vi.fn(),
    useUpdateWebhook: vi.fn(),
    useUserRegions: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/webhooks/$id/index";
import {
  useWebhook,
  useDeleteWebhook,
  useTestWebhook,
  useEnableWebhook,
  useDisableWebhook,
  useUpdateWebhook,
  useUserRegions,
} from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderDetail() {
  const Component = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

const testMutate = vi.fn();
const enableMutate = vi.fn();
const disableMutate = vi.fn();
const deleteMutate = vi.fn();
const updateMutate = vi.fn();

const baseWebhook = {
  id: "wh-1",
  ownerUserId: "u1",
  name: "ci-deploy",
  targetUrl: "https://ci.example.com/hooks/basement",
  events: ["object.created", "object.deleted"],
  bucketFilter: { regionId: "r-home", bucket: "photos" },
  prefixFilter: "uploads/",
  hasSecret: true,
  enabled: true,
  createdAt: "2026-05-22T00:00:00Z",
  updatedAt: "2026-05-22T10:00:00Z",
  lastDelivery: {
    deliveredAt: "2026-05-22T10:00:00Z",
    httpStatus: 200,
    durationMs: 84,
    success: true,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  testMutate.mockReset();
  enableMutate.mockReset();
  disableMutate.mockReset();
  deleteMutate.mockReset();
  updateMutate.mockReset();
  navigateMock.mockReset();

  vi.mocked(useWebhook).mockReturnValue({
    data: baseWebhook,
    isLoading: false,
    error: null,
  } as any);

  vi.mocked(useTestWebhook).mockReturnValue({
    mutateAsync: testMutate,
    isPending: false,
    data: null,
    error: null,
  } as any);

  vi.mocked(useEnableWebhook).mockReturnValue({
    mutateAsync: enableMutate,
    isPending: false,
  } as any);

  vi.mocked(useDisableWebhook).mockReturnValue({
    mutateAsync: disableMutate,
    isPending: false,
  } as any);

  vi.mocked(useDeleteWebhook).mockReturnValue({
    mutateAsync: deleteMutate,
    isPending: false,
  } as any);

  vi.mocked(useUpdateWebhook).mockReturnValue({
    mutateAsync: updateMutate,
    isPending: false,
    error: null,
  } as any);

  vi.mocked(useUserRegions).mockReturnValue({
    data: [
      { id: "r-home", alias: "home", endpoint: "http://garage.lan:3902" },
    ],
    isLoading: false,
    error: null,
  } as any);
});

describe("WebhookDetailPage", () => {
  it("renders the configuration card with the redacted secret status and last-delivery card", () => {
    renderDetail();

    expect(screen.getByRole("heading", { name: "ci-deploy" })).toBeInTheDocument();
    expect(screen.getByTestId("status-badge")).toHaveTextContent("enabled");

    expect(screen.getByTestId("secret-status")).toHaveTextContent(
      /redacted/,
    );
    expect(screen.getByTestId("last-delivery-detail")).toBeInTheDocument();
    expect(screen.getByTestId("last-delivery-status")).toHaveTextContent("200");
  });

  it("'Test webhook' button fires the test mutation with the id", async () => {
    testMutate.mockResolvedValueOnce({
      id: "wh-1",
      status: "queued",
      event: "object.created",
    });
    renderDetail();

    await userEvent.click(screen.getByTestId("test-button"));
    expect(testMutate).toHaveBeenCalledTimes(1);
    expect(testMutate).toHaveBeenCalledWith("wh-1");
  });

  it("'Disable' button calls the disable mutation on an enabled webhook", async () => {
    disableMutate.mockResolvedValueOnce({ ...baseWebhook, enabled: false });
    renderDetail();

    await userEvent.click(screen.getByTestId("toggle-button"));
    expect(disableMutate).toHaveBeenCalledWith("wh-1");
  });

  it("'Enable' button calls the enable mutation on a disabled webhook", async () => {
    vi.mocked(useWebhook).mockReturnValue({
      data: { ...baseWebhook, enabled: false },
      isLoading: false,
      error: null,
    } as any);
    enableMutate.mockResolvedValueOnce({ ...baseWebhook, enabled: true });

    renderDetail();
    await userEvent.click(screen.getByTestId("toggle-button"));
    expect(enableMutate).toHaveBeenCalledWith("wh-1");
  });

  it("'Delete' confirms via window.confirm and calls the delete mutation on accept", async () => {
    deleteMutate.mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderDetail();
    await userEvent.click(screen.getByTestId("delete-button"));

    expect(confirmSpy).toHaveBeenCalledTimes(1);
    expect(deleteMutate).toHaveBeenCalledWith("wh-1");
    confirmSpy.mockRestore();
  });

  it("'Delete' is a no-op when window.confirm is dismissed", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    renderDetail();
    await userEvent.click(screen.getByTestId("delete-button"));
    expect(deleteMutate).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it("'Test' is disabled when the webhook is disabled", () => {
    vi.mocked(useWebhook).mockReturnValue({
      data: { ...baseWebhook, enabled: false },
      isLoading: false,
      error: null,
    } as any);

    renderDetail();
    expect(screen.getByTestId("test-button")).toBeDisabled();
  });

  it("'Edit' opens an inline form; saving with a new secret triggers the rotate dialog", async () => {
    // The mutation returns a WebhookMintResponse-shaped payload (with
    // a non-empty `secret`) to simulate the rotate path. The detail
    // page should open the WebhookSecretShownOnceDialog with mode=rotate.
    updateMutate.mockResolvedValueOnce({
      ...baseWebhook,
      secret: "newly-rotated-secret-token-1234",
    });

    renderDetail();

    await userEvent.click(screen.getByTestId("edit-button"));
    expect(screen.getByTestId("webhook-edit-form")).toBeInTheDocument();

    await userEvent.type(
      screen.getByTestId("edit-secret-input"),
      "operator-supplied-rotation-token",
    );

    await userEvent.click(screen.getByTestId("edit-save-button"));

    expect(updateMutate).toHaveBeenCalledTimes(1);
    const call = updateMutate.mock.calls[0]![0];
    expect(call).toMatchObject({
      id: "wh-1",
      body: expect.objectContaining({
        secret: "operator-supplied-rotation-token",
      }),
    });

    // Rotate dialog renders with the new secret + the rotate copy.
    expect(
      await screen.findByText(
        /Save your new webhook secret — it won't be shown again/,
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wh-secret")).toBeInTheDocument();
  });
});
