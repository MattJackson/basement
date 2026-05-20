import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { AttachKeyToBucketDialog } from "../AttachKeyToBucketDialog";

describe("AttachKeyToBucketDialog", () => {
  const baseProps = {
    open: true,
    candidates: [
      { keyId: "k1", label: "ci-runner (GK1234567890…)" },
      { keyId: "k2", label: "backup-only (GK0987654321…)" },
    ],
    onSubmit: vi.fn(),
    onCancel: vi.fn(),
  };

  beforeEach(() => {
    baseProps.onSubmit.mockReset();
    baseProps.onCancel.mockReset();
  });

  it("renders title + candidate keys", () => {
    render(<AttachKeyToBucketDialog {...baseProps} />);
    expect(screen.getByText("Attach key to bucket")).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /ci-runner/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /backup-only/ })).toBeInTheDocument();
  });

  it("submits the picked key with default read grant", () => {
    render(<AttachKeyToBucketDialog {...baseProps} />);
    fireEvent.click(screen.getByRole("button", { name: /attach key/i }));
    expect(baseProps.onSubmit).toHaveBeenCalledWith({
      keyId: "k1",
      read: true,
      write: false,
      owner: false,
    });
  });

  it("write toggle clears owner if owner was checked", () => {
    render(<AttachKeyToBucketDialog {...baseProps} />);
    const ownerLabel = screen.getByText("Owner", { exact: false }).closest("label")!;
    fireEvent.click(ownerLabel);
    const writeLabel = screen.getByText("Write", { exact: false }).closest("label")!;
    fireEvent.click(writeLabel);
    fireEvent.click(screen.getByRole("button", { name: /attach key/i }));
    expect(baseProps.onSubmit).toHaveBeenCalledWith({
      keyId: "k1",
      read: false,
      write: true,
      owner: false,
    });
  });

  it("shows empty-state copy + Close when no candidates", () => {
    render(<AttachKeyToBucketDialog {...baseProps} candidates={[]} />);
    expect(
      screen.getByText(/every key in this cluster is already attached/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /close/i })).toBeInTheDocument();
  });
});
