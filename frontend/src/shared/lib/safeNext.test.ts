import { describe, it, expect } from "vitest";
import { sanitizeNextPath } from "./safeNext";

describe("sanitizeNextPath", () => {
  it("accepts a normal in-app path", () => {
    expect(sanitizeNextPath("/files")).toBe("/files");
    expect(sanitizeNextPath("/admin/clusters/abc")).toBe("/admin/clusters/abc");
    expect(sanitizeNextPath("/files?prefix=x&y=1#frag")).toBe("/files?prefix=x&y=1#frag");
  });

  it("accepts root", () => {
    expect(sanitizeNextPath("/")).toBe("/");
  });

  it("rejects protocol-relative open-redirect (//host)", () => {
    expect(sanitizeNextPath("//evil.com")).toBeNull();
    expect(sanitizeNextPath("//evil.com/path")).toBeNull();
  });

  it("rejects backslash-variant open-redirect (/\\host)", () => {
    expect(sanitizeNextPath("/\\evil.com")).toBeNull();
    expect(sanitizeNextPath("/\\/evil.com")).toBeNull();
  });

  it("rejects absolute off-origin URLs", () => {
    expect(sanitizeNextPath("https://evil.com")).toBeNull();
    expect(sanitizeNextPath("http://evil.com")).toBeNull();
    expect(sanitizeNextPath("javascript:alert(1)")).toBeNull();
  });

  it("rejects paths not starting with /", () => {
    expect(sanitizeNextPath("files")).toBeNull();
    expect(sanitizeNextPath("evil.com")).toBeNull();
    expect(sanitizeNextPath("")).toBeNull();
  });

  it("rejects non-string input", () => {
    expect(sanitizeNextPath(undefined)).toBeNull();
    expect(sanitizeNextPath(null)).toBeNull();
    expect(sanitizeNextPath(123)).toBeNull();
    expect(sanitizeNextPath(["/files"])).toBeNull();
  });
});
