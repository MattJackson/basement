import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";

import { ObjectRow } from "@/components/objects/ObjectRow";

const mockObject: any = {
  key: "test-file.txt",
  size: 1024,
  lastModified: new Date("2024-05-20T10:30:00Z"),
  etag: "abc123",
  isDir: false,
};

describe("ObjectRow", () => {
  it("renders file row with download button", () => {
    render(
      <table>
        <tbody>
          <ObjectRow object={mockObject} onDownload={() => {}} />
        </tbody>
      </table>,
    );

    expect(screen.getByText("test-file.txt")).toBeInTheDocument();
    expect(screen.getByText("1.0 KB")).toBeInTheDocument();
    expect(screen.getByText(/just now/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /download/i })).toBeInTheDocument();
  });

  it("renders folder row with folder icon and click handler", () => {
    const mockFolder = {
      ...mockObject,
      key: "test-folder/",
      isDir: true,
    };

    const onFolderClick = vi.fn();

    render(
      <table>
        <tbody>
          <ObjectRow object={mockFolder} isFolder onFolderClick={onFolderClick} />
        </tbody>
      </table>,
    );

    expect(screen.getByText("test-folder")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /download/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/KB/)).not.toBeInTheDocument();

    const row = screen.getByRole("row");
    row.click();

    expect(onFolderClick).toHaveBeenCalledWith("test-folder/");
  });

  it("renders blank values when size and timestamp are null", () => {
    const mockObjectWithNulls: any = {
      key: "file.txt",
      size: null,
      lastModified: null,
      isDir: false,
    };

    render(
      <table>
        <tbody>
          <ObjectRow object={mockObjectWithNulls} onDownload={() => {}} />
        </tbody>
      </table>,
    );

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("handles humanized time formatting for older dates", () => {
    const oldDate = new Date("2024-01-01T10:30:00Z");
    const mockObjectOld: any = {
      ...mockObject,
      lastModified: oldDate,
    };

    render(
      <table>
        <tbody>
          <ObjectRow object={mockObjectOld} onDownload={() => {}} />
        </tbody>
      </table>,
    );

    // Should show relative time like "Xd ago" or date format for old dates
    const timeCell = screen.getAllByText(/—|[0-9]+d ago|[A-Z][a-z]{2} [0-9]+/);
    expect(timeCell.length).toBeGreaterThan(0);
  });

  it("handles large file sizes", () => {
    const mockLargeObject: any = {
      ...mockObject,
      size: 1572864, // 1.5 MB
    };

    render(
      <table>
        <tbody>
          <ObjectRow object={mockLargeObject} onDownload={() => {}} />
        </tbody>
      </table>,
    );

    expect(screen.getByText("1.5 MB")).toBeInTheDocument();
  });
});
