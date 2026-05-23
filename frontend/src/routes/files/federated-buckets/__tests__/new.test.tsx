// Tests for the /files/federated-buckets/new wizard (v1.6.0d).
//
// Drives the 5-step wizard via userEvent and asserts:
//   - Step 1 + 2 + 3 validation gates the Next button
//   - Duplicate (region, bucket) across primary + replicas surfaces
//     a client-side warning and disables Next
//   - Step 5 submit posts the canonical create body and navigates
//     to the new federation's detail page

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
    useCreateFederation: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/federated-buckets/new";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateFederation,
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

const mutateMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mutateMock.mockReset();
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
    // The same bucket list against any region — enough for the test to
    // exercise the "can't pick the same target twice" rule below.
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

  vi.mocked(useCreateFederation).mockReturnValue({
    mutate: mutateMock,
    isPending: false,
    error: null,
  } as any);
});

describe("NewFederationPage (/files/federated-buckets/new)", () => {
  // v1.10.0.1 — pre-fix the Next button was disabled on incomplete
  // steps; an operator clicking Next on Step 1 with blank dropdowns
  // got no feedback at all (smoke caught it). The new behaviour:
  // Next stays enabled, click surfaces an inline `role=alert` error
  // describing the missing field, the wizard refuses to advance.
  it("renders Step 1 (Primary) on mount and surfaces inline error when Next is clicked blank", async () => {
    renderWizard();

    expect(screen.getByText(/Step 1 of 5/)).toBeInTheDocument();
    const nextBtn = screen.getByRole("button", { name: "Next" });
    expect(nextBtn).toBeEnabled();

    await userEvent.click(nextBtn);
    expect(screen.getByTestId("step-error")).toHaveTextContent(/primary region/i);
    // Still on Step 1.
    expect(screen.getByText(/Step 1 of 5/)).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText(/Region \*/), "r-home");
    await userEvent.click(nextBtn);
    expect(screen.getByTestId("step-error")).toHaveTextContent(/primary bucket/i);
    expect(screen.getByText(/Step 1 of 5/)).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText(/Bucket \*/), "photos");
    await userEvent.click(nextBtn);
    expect(screen.getByText(/Step 2 of 5/)).toBeInTheDocument();
  });

  it("advances through Step 2 (Replicas) and shows an error when the row is empty", async () => {
    renderWizard();

    // Step 1
    await userEvent.selectOptions(screen.getByLabelText(/Region \*/), "r-home");
    await userEvent.selectOptions(screen.getByLabelText(/Bucket \*/), "photos");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    expect(screen.getByText(/Step 2 of 5/)).toBeInTheDocument();
    const nextBtn = screen.getByRole("button", { name: "Next" });
    // Replica row starts empty -> clicking Next surfaces an inline error.
    await userEvent.click(nextBtn);
    expect(screen.getByTestId("step-error")).toHaveTextContent(/region and a bucket/i);
    expect(screen.getByText(/Step 2 of 5/)).toBeInTheDocument();

    await userEvent.selectOptions(
      screen.getByLabelText("Region", { selector: "#replica-region-0" }),
      "r-b2",
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Bucket", { selector: "#replica-bucket-0" }),
      "docs",
    );
    await userEvent.click(nextBtn);
    expect(screen.getByText(/Step 3 of 5/)).toBeInTheDocument();
  });

  it("flags a duplicate (region, bucket) pair between primary + replica and blocks Next", async () => {
    renderWizard();

    // Primary: r-home / photos
    await userEvent.selectOptions(screen.getByLabelText(/Region \*/), "r-home");
    await userEvent.selectOptions(screen.getByLabelText(/Bucket \*/), "photos");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    // Try to add a replica with the same (r-home, photos) — the bucket
    // option is disabled in the dropdown so we test via the warning
    // banner that the duplicate detection surfaces. The option element
    // remains selectable in jsdom even when disabled, so the warning
    // exercises the duplicate-state branch.
    await userEvent.selectOptions(
      screen.getByLabelText("Region", { selector: "#replica-region-0" }),
      "r-home",
    );
    // The bucket option is rendered with disabled=true for the taken
    // target; userEvent will fall through to "select anyway" in jsdom,
    // which is exactly the state we need to assert the warning fires.
    const bucketSelect = screen.getByLabelText("Bucket", {
      selector: "#replica-bucket-0",
    }) as HTMLSelectElement;
    bucketSelect.value = "photos";
    bucketSelect.dispatchEvent(new Event("change", { bubbles: true }));

    expect(screen.getByTestId("duplicate-replica-warning")).toBeInTheDocument();
    // Clicking Next surfaces the duplicate error and refuses to advance.
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByTestId("step-error")).toHaveTextContent(/duplicated/i);
    expect(screen.getByText(/Step 2 of 5/)).toBeInTheDocument();
  });

  it("submits the canonical create body on Step 5 and navigates to the detail page", async () => {
    mutateMock.mockImplementation((_body, opts) => {
      opts.onSuccess?.({ id: "fb-new", name: "photos" });
    });

    renderWizard();

    // Step 1
    await userEvent.selectOptions(screen.getByLabelText(/Region \*/), "r-home");
    await userEvent.selectOptions(screen.getByLabelText(/Bucket \*/), "photos");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    // Step 2
    await userEvent.selectOptions(
      screen.getByLabelText("Region", { selector: "#replica-region-0" }),
      "r-b2",
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Bucket", { selector: "#replica-bucket-0" }),
      "photos",
    );
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    // Step 3 - defaults work
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    // Step 4 - name required
    await userEvent.type(screen.getByLabelText(/Federation name/), "photos");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    // Step 5
    await userEvent.click(screen.getByTestId("create-submit"));

    expect(mutateMock).toHaveBeenCalledTimes(1);
    const call = mutateMock.mock.calls[0];
    expect(call).toBeDefined();
    const body = call![0];
    expect(body).toMatchObject({
      name: "photos",
      primary: { regionId: "r-home", bucket: "photos" },
      replicas: [{ regionId: "r-b2", bucket: "photos" }],
      policy: {
        syncMode: "continuous",
        lagAlertSec: 300,
        writeQuorum: 1,
      },
    });
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/files/federated-buckets/$id",
      params: { id: "fb-new" },
    });
  });
});
