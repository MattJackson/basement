import { describe, it, expect } from "vitest";
import { isSignupEnabled } from "@/shared/api/queries";

describe("isSignupEnabled", () => {
  it("returns true when signupMode is 'open'", () => {
    expect(isSignupEnabled("open")).toBe(true);
  });

  it("returns true when signupMode is 'invite'", () => {
    expect(isSignupEnabled("invite")).toBe(true);
  });

  it("returns false when signupMode is 'closed'", () => {
    expect(isSignupEnabled("closed")).toBe(false);
  });

  it("returns false when signupMode is undefined", () => {
    expect(isSignupEnabled(undefined)).toBe(false);
  });

  it("returns false for any other value", () => {
    expect(isSignupEnabled("unknown" as any)).toBe(false);
  });
});
