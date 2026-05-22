// Tests for the /files/keys/new "Add a key" form (v1.2.0d).
//
// Render the page in isolation, drive the form via userEvent, assert
// the useCreateUserKey mutation is called with the trimmed payload
// and that the user navigates back to /files on success.

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => () => ({}),
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useCreateUserKey: vi.fn(),
    // v1.3.0b: form pulls driver defaults for the "Common endpoints"
    // expandable; stub so the test doesn't fire a real fetch.
    useDriverDefaults: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import AddKeyPage from "@/routes/files/keys/new";
import { useCreateUserKey, useDriverDefaults } from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderWithProviders(node: React.ReactNode) {
  return render(
    <QueryClientProvider client={queryClient}>{node}</QueryClientProvider>,
  );
}

const mutateAsyncMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mutateAsyncMock.mockReset();
  navigateMock.mockReset();
  vi.mocked(useCreateUserKey).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
  vi.mocked(useDriverDefaults).mockReturnValue({
    data: [
      {
        driver: "garage-v1",
        displayName: "Garage v1",
        adminUrl: "http://garage-host:3903",
        adminUrlHint: "Garage admin API, default port 3903.",
        s3Endpoint: "http://garage-host:3902",
        s3EndpointHint: "Garage S3 API, default port 3902.",
        regionLabel: "garage",
      },
      {
        driver: "aws-s3",
        displayName: "AWS S3",
        adminUrl: "",
        adminUrlHint: "AWS S3 has no admin URL — leave blank.",
        s3Endpoint: "https://s3.us-east-1.amazonaws.com",
        s3EndpointHint: "AWS S3 regional endpoint; substitute your region.",
        regionLabel: "us-east-1",
      },
    ],
    isLoading: false,
    error: null,
  } as any);
});

describe("AddKeyPage (/files/keys/new)", () => {
  it("renders the 'Add a key' heading and form fields", () => {
    renderWithProviders(<AddKeyPage />);

    expect(
      screen.getByRole("heading", { name: "Add a key" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/Key alias/)).toBeInTheDocument();
    expect(screen.getByLabelText(/S3 endpoint URL/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Access Key ID/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Secret Access Key/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add key/ })).toBeInTheDocument();
  });

  it("disables submit until required fields are filled", async () => {
    renderWithProviders(<AddKeyPage />);

    const submit = screen.getByRole("button", { name: /Add key/ });
    expect(submit).toBeDisabled();

    await userEvent.type(screen.getByLabelText(/Key alias/), "home");
    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "https://s3.example.com",
    );
    await userEvent.type(screen.getByLabelText(/Access Key ID/), "GK_TEST");
    await userEvent.type(screen.getByLabelText(/Secret Access Key/), "sssh");

    expect(submit).toBeEnabled();
  });

  it("submits the trimmed payload and navigates back to /files on success", async () => {
    mutateAsyncMock.mockResolvedValueOnce({
      id: "r-new",
      userId: "matthew",
      alias: "home",
      endpoint: "https://s3.example.com",
      region: "us-east-1",
      accessKeyId: "GK_TEST",
      createdAt: "2026-05-21T00:00:00Z",
      updatedAt: "2026-05-21T00:00:00Z",
    });

    renderWithProviders(<AddKeyPage />);

    // Whitespace around inputs should be trimmed by the submit path.
    await userEvent.type(screen.getByLabelText(/Key alias/), "  home  ");
    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "  https://s3.example.com  ",
    );
    await userEvent.type(
      screen.getByLabelText(/Access Key ID/),
      "  GK_TEST  ",
    );
    await userEvent.type(screen.getByLabelText(/Secret Access Key/), "sssh");

    await userEvent.click(screen.getByRole("button", { name: /Add key/ }));

    expect(mutateAsyncMock).toHaveBeenCalledTimes(1);
    expect(mutateAsyncMock).toHaveBeenCalledWith({
      alias: "home",
      endpoint: "https://s3.example.com",
      accessKeyId: "GK_TEST",
      secretKey: "sssh",
      region: "us-east-1",
      // v1.3.0c: path-style is the default when the virtual-host toggle
      // hasn't been flipped.
      addressingStyle: "path",
    });
    expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
  });

  // v1.3.0c — Advanced expandable with the addressing-style toggle.
  it("submits addressingStyle=virtual_host when the Advanced toggle is checked", async () => {
    mutateAsyncMock.mockResolvedValueOnce({ id: "r-new" });

    renderWithProviders(<AddKeyPage />);

    await userEvent.type(screen.getByLabelText(/Key alias/), "home");
    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "https://s3.pq.io",
    );
    await userEvent.type(screen.getByLabelText(/Access Key ID/), "GK_TEST");
    await userEvent.type(screen.getByLabelText(/Secret Access Key/), "sssh");

    await userEvent.click(screen.getByTestId("toggle-advanced"));
    await userEvent.click(screen.getByTestId("virtual-host-toggle"));

    await userEvent.click(screen.getByRole("button", { name: /Add key/ }));

    expect(mutateAsyncMock).toHaveBeenCalledTimes(1);
    expect(mutateAsyncMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ addressingStyle: "virtual_host" }),
    );
  });

  // v1.3.0c — IP-host smart default: the toggle is disabled (cannot be
  // checked) when the endpoint host is an IP literal. Server-side
  // BuildS3Client also enforces this, but the FE disables to give an
  // operator immediate, targeted feedback.
  it("disables the virtual-host toggle when the endpoint host is an IP", async () => {
    renderWithProviders(<AddKeyPage />);

    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "http://10.1.7.10:3902",
    );
    await userEvent.click(screen.getByTestId("toggle-advanced"));

    const toggle = screen.getByTestId(
      "virtual-host-toggle",
    ) as HTMLInputElement;
    expect(toggle).toBeDisabled();
  });

  // v1.3.0c — AWS row in "Common endpoints" auto-checks the
  // virtual-host toggle (AWS prefers it for tooling compat).
  it("auto-checks the virtual-host toggle when AWS is picked from Common endpoints", async () => {
    renderWithProviders(<AddKeyPage />);

    await userEvent.click(
      screen.getByRole("button", { name: /Common endpoints/ }),
    );
    await userEvent.click(screen.getByTestId("use-endpoint-aws-s3"));

    await userEvent.click(screen.getByTestId("toggle-advanced"));
    const toggle = screen.getByTestId(
      "virtual-host-toggle",
    ) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
  });

  // v1.3.0b — Common endpoints expandable + endpoint-driven region
  // auto-suggest. Operator-typed values must never be overwritten
  // silently; the apply-example button is the only auto-fill path.
  it("auto-suggests region label when the endpoint matches a known pattern", async () => {
    renderWithProviders(<AddKeyPage />);

    const regionInput = screen.getByLabelText(/S3 region/) as HTMLInputElement;
    expect(regionInput.value).toBe("us-east-1");

    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "http://garage.lan:3902",
    );
    expect(regionInput.value).toBe("garage");
  });

  it("does not overwrite the region once the operator has typed one", async () => {
    renderWithProviders(<AddKeyPage />);
    const regionInput = screen.getByLabelText(/S3 region/) as HTMLInputElement;

    await userEvent.clear(regionInput);
    await userEvent.type(regionInput, "fr-par");

    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "http://garage.lan:3902",
    );
    expect(regionInput.value).toBe("fr-par");
  });

  it("renders a 'Common endpoints' expandable and fills both endpoint + region on Use this", async () => {
    renderWithProviders(<AddKeyPage />);

    await userEvent.click(screen.getByRole("button", { name: /Common endpoints/ }));
    const useGarage = screen.getByTestId("use-endpoint-garage-v1");
    await userEvent.click(useGarage);

    const endpointInput = screen.getByLabelText(/S3 endpoint URL/) as HTMLInputElement;
    const regionInput = screen.getByLabelText(/S3 region/) as HTMLInputElement;
    expect(endpointInput.value).toBe("http://garage-host:3902");
    expect(regionInput.value).toBe("garage");
  });

  it("renders the server error message when the mutation rejects", async () => {
    mutateAsyncMock.mockRejectedValueOnce(new Error("duplicate alias"));

    renderWithProviders(<AddKeyPage />);

    await userEvent.type(screen.getByLabelText(/Key alias/), "home");
    await userEvent.type(
      screen.getByLabelText(/S3 endpoint URL/),
      "https://s3.example.com",
    );
    await userEvent.type(screen.getByLabelText(/Access Key ID/), "GK_TEST");
    await userEvent.type(screen.getByLabelText(/Secret Access Key/), "sssh");

    await userEvent.click(screen.getByRole("button", { name: /Add key/ }));

    expect(await screen.findByTestId("add-key-error")).toHaveTextContent(
      "duplicate alias",
    );
    expect(navigateMock).not.toHaveBeenCalled();
  });
});
