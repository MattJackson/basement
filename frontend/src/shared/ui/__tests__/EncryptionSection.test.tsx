// v1.10.0d — EncryptionSection coverage.
//
// Four branches:
//   - both supported flags false → "Unsupported" notice
//   - only SSE-S3 supported → only-S3 notice, no radio
//   - both supported → full editor (radio + KMS key + bucket key)
//   - already-enabled state hydrates the form from the wire shape

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { EncryptionSection } from "@/shared/ui/EncryptionSection";

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({
  client,
  children,
}: {
  client: QueryClient;
  children: React.ReactNode;
}) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const originalFetch = globalThis.fetch;

beforeEach(() => {
  // Default: no responses. Each test overrides per case.
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

function mockEncryptionFetch(payload: {
  enabled: boolean;
  supportedS3: boolean;
  supportedKms: boolean;
  algorithm?: string;
  kmsKeyId?: string;
  bucketKey?: boolean;
}) {
  globalThis.fetch = vi.fn(async (url: RequestInfo | URL) => {
    const s = String(url);
    if (s.includes("/encryption")) {
      return new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;
}

describe("EncryptionSection (v1.10.0d)", () => {
  it("renders the unsupported notice when the driver supports neither SSE flavor", async () => {
    mockEncryptionFetch({
      enabled: false,
      supportedS3: false,
      supportedKms: false,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-unsupported")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("encryption-save")).not.toBeInTheDocument();
  });

  it("shows the SSE-S3-only notice (no radio) when only SSE-S3 is supported", async () => {
    mockEncryptionFetch({
      enabled: false,
      supportedS3: true,
      supportedKms: false,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-only-s3-notice")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("encryption-radio-s3")).not.toBeInTheDocument();
    expect(screen.queryByTestId("encryption-radio-kms")).not.toBeInTheDocument();
    expect(screen.getByTestId("encryption-save")).toBeInTheDocument();
  });

  it("renders the full editor with both radio options when both are supported", async () => {
    mockEncryptionFetch({
      enabled: false,
      supportedS3: true,
      supportedKms: true,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-radio-s3")).toBeInTheDocument();
    });
    expect(screen.getByTestId("encryption-radio-kms")).toBeInTheDocument();
    expect(screen.getByTestId("encryption-status-badge")).toHaveTextContent(/Disabled/i);
  });

  it("switching to SSE-KMS surfaces the KMS key input + bucket key toggle", async () => {
    mockEncryptionFetch({
      enabled: false,
      supportedS3: true,
      supportedKms: true,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-radio-kms")).toBeInTheDocument();
    });

    // KMS key input + bucket-key toggle hidden when SSE-S3 selected.
    expect(screen.queryByTestId("encryption-kms-key-input")).not.toBeInTheDocument();

    await userEvent.click(screen.getByTestId("encryption-radio-kms"));

    expect(screen.getByTestId("encryption-kms-key-input")).toBeInTheDocument();
    expect(screen.getByTestId("encryption-bucket-key-toggle")).toBeInTheDocument();
  });

  it("disables Save when SSE-KMS is selected but the key field is empty", async () => {
    mockEncryptionFetch({
      enabled: false,
      supportedS3: true,
      supportedKms: true,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-radio-kms")).toBeInTheDocument();
    });

    await userEvent.click(screen.getByTestId("encryption-radio-kms"));

    expect(screen.getByTestId("encryption-save")).toBeDisabled();

    // Filling the key re-enables Save.
    await userEvent.type(
      screen.getByTestId("encryption-kms-key-input"),
      "arn:aws:kms:us-east-1:111122223333:key/abcd",
    );
    expect(screen.getByTestId("encryption-save")).not.toBeDisabled();
  });

  it("hydrates the form from an existing SSE-KMS config", async () => {
    mockEncryptionFetch({
      enabled: true,
      supportedS3: true,
      supportedKms: true,
      algorithm: "aws:kms",
      kmsKeyId: "arn:aws:kms:us-east-1:111122223333:key/abcd",
      bucketKey: true,
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <EncryptionSection regionId="r1" bucket="b1" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("encryption-status-badge")).toHaveTextContent(/Enabled/i);
    });

    // SSE-KMS radio should be selected, key + bucket key inputs visible.
    const kmsRadio = screen.getByTestId("encryption-radio-kms") as HTMLInputElement;
    expect(kmsRadio.checked).toBe(true);
    const keyInput = screen.getByTestId("encryption-kms-key-input") as HTMLInputElement;
    expect(keyInput.value).toContain("arn:aws:kms");
    const bucketKey = screen.getByTestId("encryption-bucket-key-toggle") as HTMLInputElement;
    expect(bucketKey.checked).toBe(true);

    // Disable button surfaces on the enabled state.
    expect(screen.getByTestId("encryption-disable")).toBeInTheDocument();
  });
});
