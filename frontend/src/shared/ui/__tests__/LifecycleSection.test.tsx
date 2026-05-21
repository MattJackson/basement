import { describe, it, expect } from "vitest";
import { describeRule } from "../LifecycleSection";

// describeRule is the human-language paraphrase that runs on every
// rule row. It carries the "would an operator actually understand this
// at 2am?" weight, so it gets its own focused unit test rather than
// being asserted via React Testing Library.

describe("describeRule", () => {
  it("describes an expiration rule with a prefix", () => {
    const s = describeRule({
      id: "r",
      status: "Enabled",
      prefix: "logs/",
      expirationDays: 30,
    });
    expect(s).toContain("Delete objects after 30 days");
    expect(s).toContain("logs/");
  });

  it("singularises 1 day", () => {
    const s = describeRule({
      id: "r",
      status: "Enabled",
      expirationDays: 1,
    });
    expect(s).toContain("1 day");
    expect(s).not.toContain("1 days");
  });

  it("composes multiple actions with semicolons", () => {
    const s = describeRule({
      id: "r",
      status: "Enabled",
      expirationDays: 30,
      abortMultipartDays: 7,
    });
    expect(s.toLowerCase()).toContain("delete objects after 30 days");
    expect(s.toLowerCase()).toContain("cancel incomplete uploads after 7 days");
    expect(s).toContain(";");
  });

  it("describes a tier transition", () => {
    const s = describeRule({
      id: "r",
      status: "Enabled",
      transitionDays: 14,
      transitionTier: "GLACIER",
    });
    expect(s).toContain("Move objects to GLACIER after 14 days");
  });

  it("handles an empty rule sanely", () => {
    const s = describeRule({ id: "r", status: "Disabled" });
    expect(s).toContain("(no actions configured)");
  });

  it("describes a noncurrent-version rule", () => {
    const s = describeRule({
      id: "r",
      status: "Enabled",
      noncurrentDays: 90,
    });
    expect(s.toLowerCase()).toContain("delete noncurrent versions after 90 days");
  });
});
