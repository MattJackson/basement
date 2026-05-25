import { describe, it, expect, beforeEach } from "vitest";

describe("i18n configuration", () => {
  beforeEach(() => {
    vi.resetModules();
    window.localStorage.clear();
  });

  it("loads English and Spanish translations successfully", async () => {
    const i18n = await import("@/shared/i18n");
    
    expect(i18n.default).toBeDefined();
    expect(i18n.SUPPORTED_LANGUAGES).toEqual(["en", "es"]);
  });

  it("fallbacks to English when Spanish key is missing", async () => {
    const i18nModule = await import("@/shared/i18n");
    
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.exists("common:buttons.save")).toBe(true);
  });

  it("persists language choice to window.localStorage", async () => {
    const i18nModule = await import("@/shared/i18n");
    
    await i18nModule.default.changeLanguage("es");
    
    expect(window.localStorage.getItem("basement_language")).toBe("es");
  });

  it("loads pages namespace translations for /files page", async () => {
    const i18nModule = await import("@/shared/i18n");
    
    await i18nModule.default.changeLanguage("en");
    
    expect(i18nModule.default.exists("pages:filesHome.pageTitle")).toBe(true);
  });

  it("handles language detection from window.localStorage", async () => {
    window.localStorage.setItem("basement_language", "es");
    
    const i18nModule = await import("@/shared/i18n");
    
    expect(i18nModule.default.language).toBe("es");
  });
});
