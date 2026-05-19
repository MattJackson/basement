import { describe, it, expect } from "vitest";
import { humanizeBytes, humanizeTime } from "./format";

describe("humanizeBytes", () => {
  it.each([
    [0, "0.0 B"],
    [1, "1.0 B"],
    [512, "512.0 B"],
    [1024, "1.0 KB"],
    [1536, "1.5 KB"],
    [1048576, "1.0 MB"],
    [1572864, "1.5 MB"],
    [1073741824, "1.0 GB"],
    [1610612736, "1.5 GB"],
    [1099511627776, "1.0 TB"],
    [1649267441664, "1.5 TB"],
  ])("converts %d bytes to '%s'", (input, expected) => {
    expect(humanizeBytes(input)).toBe(expected);
  });

  it("handles large numbers correctly", () => {
    const tebibyte = 1099511627776;
    expect(humanizeBytes(tebibyte)).toBe("1.0 TB");
  });
});

describe("humanizeTime", () => {
  it("returns 'just now' for timestamps within the last minute", () => {
    const now = new Date();
    const recent = new Date(now.getTime() - 30 * 1000); // 30 seconds ago
    expect(humanizeTime(recent.toISOString())).toBe("just now");
  });

  it("returns minutes ago for timestamps within the last hour", () => {
    const now = new Date();
    const recent = new Date(now.getTime() - 5 * 60 * 1000); // 5 minutes ago
    expect(humanizeTime(recent.toISOString())).toBe("5m ago");
  });

  it("returns hours ago for timestamps within the last day", () => {
    const now = new Date();
    const recent = new Date(now.getTime() - 3 * 60 * 60 * 1000); // 3 hours ago
    expect(humanizeTime(recent.toISOString())).toBe("3h ago");
  });

  it("returns days ago for timestamps within the last week", () => {
    const now = new Date();
    const recent = new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000); // 2 days ago
    expect(humanizeTime(recent.toISOString())).toBe("2d ago");
  });

  it("returns formatted date for older timestamps", () => {
    const oldDate = new Date("2024-01-15T10:30:00Z");
    const result = humanizeTime(oldDate.toISOString());
    expect(result).toMatch(/Jan\s+15(?:,\s+\d{4})?/);
  });
});
