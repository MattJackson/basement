// UploadDialog coverage (audit fe2 — previously zero tests).
//
// Focus is the stale-closure correctness bugs the audit flagged:
//   - all-succeed → onSuccess() fires (was: stale `files` snapshot read
//     after `await Promise.all` meant allDone was almost always false and
//     onSuccess never fired).
//   - "Retry Failed" genuinely re-runs errored uploads (was: it filtered
//     out cancelled rows and did NOT retry failures).
//
// UploadDialog talks to the network via raw fetch (presign + PUT), so we
// stub globalThis.fetch. Files here are all <= 5 MB → single-shot path.

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, afterEach, vi } from "vitest";

import { UploadDialog } from "@/components/upload/UploadDialog";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

function makeFile(name: string, bytes = 16): File {
  return new File([new Uint8Array(bytes)], name, { type: "text/plain" });
}

/**
 * installFetch wires presign + PUT. `putShouldFail` lets a test make the
 * PUT fail on the FIRST call (per object key) and then succeed on retry,
 * so we can exercise the "Retry Failed" path.
 */
function installFetch(opts: { failPutOnce?: boolean } = {}) {
  const putAttempts = new Map<string, number>();
  const fetchMock = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
    const s = String(url);
    if (s.includes("/presign-put")) {
      return new Response(JSON.stringify({ url: "https://signed.example/put?obj=x" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    // The PUT to the signed URL.
    if (init?.method === "PUT") {
      if (opts.failPutOnce) {
        const n = (putAttempts.get(s) ?? 0) + 1;
        putAttempts.set(s, n);
        if (n === 1) {
          return new Response("boom", { status: 403 });
        }
      }
      return new Response(null, { status: 200 });
    }
    return new Response("not found", { status: 404 });
  });
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return fetchMock;
}

async function addFiles(files: File[]) {
  const input = document.getElementById("file-input") as HTMLInputElement;
  await userEvent.upload(input, files);
}

describe("UploadDialog", () => {
  it("fires onSuccess once every file upload completes", async () => {
    installFetch();
    const onSuccess = vi.fn();

    render(
      <UploadDialog
        open
        onOpenChange={() => {}}
        regionId="r1"
        bid="bucket-a"
        prefix="folder/"
        onSuccess={onSuccess}
      />,
    );

    await addFiles([makeFile("a.txt"), makeFile("b.txt")]);

    const uploadBtn = await screen.findByRole("button", { name: /Upload 2 Files/i });
    await userEvent.click(uploadBtn);

    // Both rows reach "Done" and onSuccess fires exactly once.
    await waitFor(() => {
      expect(screen.getAllByText("Done")).toHaveLength(2);
    });
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  it("Retry Failed re-runs errored uploads (and they can then succeed)", async () => {
    installFetch({ failPutOnce: true });
    const onSuccess = vi.fn();

    render(
      <UploadDialog
        open
        onOpenChange={() => {}}
        regionId="r1"
        bid="bucket-a"
        prefix=""
        onSuccess={onSuccess}
      />,
    );

    await addFiles([makeFile("c.txt")]);

    await userEvent.click(
      await screen.findByRole("button", { name: /Upload 1 Files/i }),
    );

    // First attempt fails (403) → an error row + a "Retry Failed" button.
    const retryFailed = await screen.findByRole("button", { name: /Retry Failed/i });

    await userEvent.click(retryFailed);

    // Second attempt succeeds.
    await waitFor(() => {
      expect(screen.getByText("Done")).toBeInTheDocument();
    });
  });
});
