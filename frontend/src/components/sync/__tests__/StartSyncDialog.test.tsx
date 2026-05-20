import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { StartSyncDialog } from "@/components/sync/StartSyncDialog";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    useCreateUserSync: () => ({
      mutateAsync: vi.fn(),
      isPending: false,
    }),
  };
});

describe("StartSyncDialog", () => {
  it("renders with direction='pull'", async () => {
    render(
      <StartSyncDialog 
        open={true} 
        onOpenChange={() => {}}
        direction="pull"
      />
    );

    // Multiple elements have "sync" text (header, button, etc.) —
    // assert at least one is present rather than exactly one.
    expect(screen.getAllByText(/sync/i).length).toBeGreaterThan(0);
  });

  it("renders with direction='push' and pre-fills source", async () => {
    render(
      <StartSyncDialog 
        open={true} 
        onOpenChange={() => {}}
        direction="push"
        defaultSrcConnectionId="cluster-1"
        defaultSrcBucket="bucket-a"
      />
    );

    // Multiple elements have "sync" text (header, button, etc.) —
    // assert at least one is present rather than exactly one.
    expect(screen.getAllByText(/sync/i).length).toBeGreaterThan(0);
    
    const srcClusterSelect = screen.getByLabelText(/source cluster/i);
    expect(srcClusterSelect).toBeDisabled();
  });
});
