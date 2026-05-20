import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";

import SharesPlaceholder from "@/routes/files/shares";

describe("SharesPlaceholder", () => {
  it("renders 'Shares' heading and 'coming in v0.7' message", async () => {
    render(<SharesPlaceholder />);

    expect(screen.getByText("Shares")).toBeInTheDocument();
    expect(screen.getByText(/coming in v0\.7/)).toBeInTheDocument();
  });
});
