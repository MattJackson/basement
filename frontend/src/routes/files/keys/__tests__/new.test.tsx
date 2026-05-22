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
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import AddKeyPage from "@/routes/files/keys/new";
import { useCreateUserKey } from "@/shared/api/queries";

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
    });
    expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
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
