// Tests for the /files/webhooks/new create form (v1.7.0e).
//
// Asserts:
//   - Form gates the submit until validation passes (name regex,
//     target URL, at least one event)
//   - HTTPS warning surfaces on http:// URLs
//   - Submit posts the canonical create body
//   - On success the SecretShownOnceDialog renders with the minted
//     secret + the HMAC verification snippet

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
    }),
    Link: ({ children }: any) => <a>{children}</a>,
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegions: vi.fn(),
    useUserRegionBuckets: vi.fn(),
    useCreateWebhook: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/webhooks/new";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateWebhook,
} from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderWizard() {
  const Component = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

const mutateAsyncMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mutateAsyncMock.mockReset();
  navigateMock.mockReset();

  vi.mocked(useUserRegions).mockReturnValue({
    data: [
      { id: "r-home", alias: "home", endpoint: "http://garage.lan:3902" },
      { id: "r-b2", alias: "b2-offsite", endpoint: "https://b2.example.com" },
    ],
    isLoading: false,
    error: null,
  } as any);

  vi.mocked(useUserRegionBuckets).mockImplementation((regionId: any) => {
    return {
      data: regionId
        ? {
            buckets: [
              { id: "photos", aliases: ["photos"] },
              { id: "docs", aliases: ["docs"] },
            ],
            perBucketStatsAvailable: false,
          }
        : undefined,
    } as any;
  });

  vi.mocked(useCreateWebhook).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
});

describe("NewWebhookPage", () => {
  it("renders the form and gates submit until validation passes", async () => {
    renderWizard();

    expect(
      screen.getByRole("heading", { name: "New webhook" }),
    ).toBeInTheDocument();

    const submit = screen.getByTestId("webhook-create-submit");
    expect(submit).toBeDisabled();

    // Name alone isn't enough — target URL still missing.
    await userEvent.type(
      screen.getByTestId("webhook-name-input"),
      "ci-deploy",
    );
    expect(submit).toBeDisabled();

    // Target URL — submit enables.
    await userEvent.type(
      screen.getByTestId("webhook-target-url-input"),
      "https://ci.example.com/hooks/basement",
    );
    expect(submit).toBeEnabled();
  });

  it("surfaces an HTTPS warning for http:// URLs", async () => {
    renderWizard();

    await userEvent.type(
      screen.getByTestId("webhook-target-url-input"),
      "http://insecure.example.com/hook",
    );
    expect(screen.getByTestId("webhook-https-warning")).toBeInTheDocument();
  });

  it("disables the Bucket select until a region is picked", async () => {
    renderWizard();

    expect(screen.getByTestId("webhook-bucket-select")).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByTestId("webhook-region-select"),
      "r-home",
    );
    expect(screen.getByTestId("webhook-bucket-select")).toBeEnabled();
  });

  it("posts the canonical create body and opens the secret-shown-once dialog on success", async () => {
    mutateAsyncMock.mockResolvedValueOnce({
      id: "wh-new",
      ownerUserId: "u1",
      name: "ci-deploy",
      targetUrl: "https://ci.example.com/hooks/basement",
      events: ["object.created", "object.deleted"],
      hasSecret: true,
      enabled: true,
      createdAt: "2026-05-22T00:00:00Z",
      updatedAt: "2026-05-22T00:00:00Z",
      secret: "abcd1234abcd1234abcd1234abcd1234",
    });

    renderWizard();

    await userEvent.type(
      screen.getByTestId("webhook-name-input"),
      "ci-deploy",
    );
    await userEvent.type(
      screen.getByTestId("webhook-target-url-input"),
      "https://ci.example.com/hooks/basement",
    );

    await userEvent.click(screen.getByTestId("webhook-create-submit"));

    expect(mutateAsyncMock).toHaveBeenCalledTimes(1);
    const body = mutateAsyncMock.mock.calls[0]![0];
    expect(body).toMatchObject({
      name: "ci-deploy",
      targetUrl: "https://ci.example.com/hooks/basement",
      events: expect.arrayContaining(["object.created", "object.deleted"]),
    });

    // The shown-once dialog renders with the minted secret + HMAC
    // verification snippet, and the operator is gated on the
    // acknowledgement checkbox before Done can fire.
    expect(
      await screen.findByText(
        /Save your webhook secret — it won't be shown again/,
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wh-secret")).toBeInTheDocument();
    expect(screen.getByTestId("wh-verify-snippet")).toHaveTextContent(
      /hmac.compare_digest/,
    );

    // Done is gated until acknowledged.
    expect(screen.getByTestId("wh-done-button")).toBeDisabled();
    await userEvent.click(screen.getByTestId("wh-acknowledged-checkbox"));
    expect(screen.getByTestId("wh-done-button")).toBeEnabled();

    await userEvent.click(screen.getByTestId("wh-done-button"));
    expect(navigateMock).toHaveBeenCalledWith({ to: "/files/webhooks" });
  });

  it("event-blurbs render against every known event-type checkbox", () => {
    renderWizard();

    expect(
      screen.getByTestId("event-checkbox-object.created"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("event-checkbox-object.deleted"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("event-checkbox-object.modified"),
    ).toBeInTheDocument();
  });
});
