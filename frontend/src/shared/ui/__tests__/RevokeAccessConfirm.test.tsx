import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { RevokeAccessConfirm } from "../RevokeAccessConfirm";

describe("RevokeAccessConfirm", () => {
  const baseProps = {
    open: true,
    subject: "ci-runner",
    target: "deploy-artifacts",
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
  };

  beforeEach(() => {
    baseProps.onConfirm.mockReset();
    baseProps.onCancel.mockReset();
  });

  it("renders both sides of the edge in the description", () => {
    render(<RevokeAccessConfirm {...baseProps} />);
    expect(screen.getByText("Revoke access?")).toBeInTheDocument();
    expect(screen.getByText("ci-runner")).toBeInTheDocument();
    expect(screen.getByText("deploy-artifacts")).toBeInTheDocument();
  });

  it("fires onConfirm when Revoke is clicked", () => {
    render(<RevokeAccessConfirm {...baseProps} />);
    fireEvent.click(screen.getByRole("button", { name: /revoke access/i }));
    expect(baseProps.onConfirm).toHaveBeenCalledTimes(1);
  });

  it("fires onCancel when Cancel is clicked", () => {
    render(<RevokeAccessConfirm {...baseProps} />);
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    expect(baseProps.onCancel).toHaveBeenCalledTimes(1);
  });

  it("disables Revoke button while isRevoking", () => {
    render(<RevokeAccessConfirm {...baseProps} isRevoking />);
    expect(screen.getByRole("button", { name: /revoking/i })).toBeDisabled();
  });

  it("renders nothing when open is false", () => {
    const { container } = render(<RevokeAccessConfirm {...baseProps} open={false} />);
    expect(container.querySelector("[role='alertdialog']")).toBeNull();
  });
});
