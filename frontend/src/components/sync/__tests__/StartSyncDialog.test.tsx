import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { StartSyncDialog } from "@/components/sync/StartSyncDialog";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
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

    expect(screen.getByText(/sync/i)).toBeInTheDocument();
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

    expect(screen.getByText(/sync/i)).toBeInTheDocument();
    
    const srcClusterSelect = screen.getByLabelText(/source cluster/i);
    expect(srcClusterSelect).toBeDisabled();
  });
});
