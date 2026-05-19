import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ErrorBanner } from "../ErrorBanner";

describe("ErrorBanner", () => {
  it("renders error message correctly", () => {
    render(
      <ErrorBanner message="Connection failed" />
    );

    expect(screen.getByText("Connection failed")).toBeInTheDocument();
  });

  it("calls onRetry when clicked and handler is provided", async () => {
    const handleRetry = vi.fn();

    render(<ErrorBanner message="Connection failed" onRetry={handleRetry} />);

    await userEvent.click(screen.getByText("Connection failed"));

    expect(handleRetry).toHaveBeenCalledTimes(1);
  });

  it("does not call onRetry when no handler is provided", async () => {
    render(<ErrorBanner message="Connection failed" />);

    const banner = screen.getByText("Connection failed").closest("div");
    
    await userEvent.click(banner!);

    // Should not throw or cause errors
    expect(screen.getByText("Connection failed")).toBeInTheDocument();
  });

  it("applies correct styling classes", () => {
    render(<ErrorBanner message="Test error" />);

    const banner = screen.getByText("Test error").closest("div");
    expect(banner).toHaveClass("rounded-lg", "border", "bg-destructive/10", "p-4");
  });
});
