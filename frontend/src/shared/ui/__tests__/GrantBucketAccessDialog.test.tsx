import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { GrantBucketAccessDialog } from "../GrantBucketAccessDialog";

describe("GrantBucketAccessDialog", () => {
  const baseProps = {
    open: true,
    candidates: [
      { bucketId: "b1", label: "alpha" },
      { bucketId: "b2", label: "beta" },
    ],
    onSubmit: vi.fn(),
    onCancel: vi.fn(),
  };

  beforeEach(() => {
    baseProps.onSubmit.mockReset();
    baseProps.onCancel.mockReset();
  });

  it("renders title and the candidate buckets", () => {
    render(<GrantBucketAccessDialog {...baseProps} />);
    expect(screen.getByText("Grant bucket access")).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "alpha" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "beta" })).toBeInTheDocument();
  });

  it("submits the picked bucket with the default read-only grant", () => {
    render(<GrantBucketAccessDialog {...baseProps} />);
    fireEvent.click(screen.getByRole("button", { name: /grant access/i }));
    expect(baseProps.onSubmit).toHaveBeenCalledWith({
      bucketId: "b1",
      read: true,
      write: false,
      owner: false,
    });
  });

  it("checking Owner clears Read and Write", () => {
    render(<GrantBucketAccessDialog {...baseProps} />);
    const ownerLabel = screen.getByText("Owner", { exact: false }).closest("label")!;
    fireEvent.click(ownerLabel);
    fireEvent.click(screen.getByRole("button", { name: /grant access/i }));
    expect(baseProps.onSubmit).toHaveBeenCalledWith({
      bucketId: "b1",
      read: false,
      write: false,
      owner: true,
    });
  });

  it("disables Grant when no permission is selected", () => {
    render(<GrantBucketAccessDialog {...baseProps} />);
    // Uncheck the default Read box.
    const readLabel = screen.getByText("Read", { exact: false }).closest("label")!;
    fireEvent.click(readLabel);
    expect(screen.getByRole("button", { name: /grant access/i })).toBeDisabled();
  });

  it("renders an empty-candidates message and a Close button when there's nothing to grant", () => {
    render(<GrantBucketAccessDialog {...baseProps} candidates={[]} />);
    expect(
      screen.getByText(/already has access to every bucket/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /close/i })).toBeInTheDocument();
  });

  it("surfaces errorMessage when provided", () => {
    render(<GrantBucketAccessDialog {...baseProps} errorMessage="boom" />);
    expect(screen.getByText("boom")).toBeInTheDocument();
  });
});
