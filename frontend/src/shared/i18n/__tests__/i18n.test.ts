import { describe, it, expect, beforeEach } from "vitest";

function getAllKeys(obj: Record<string, unknown>, prefix = ""): string[] {
  const keys: string[] = [];
  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "object" && value !== null && !Array.isArray(value)) {
      keys.push(...getAllKeys(value as Record<string, unknown>, fullKey));
    } else {
      keys.push(fullKey);
    }
  }
  return keys;
}

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

  it("en and es common.json have the same key set", async () => {
    const enCommon = (await import("@/shared/i18n/locales/en/common.json")).default;
    const esCommon = (await import("@/shared/i18n/locales/es/common.json")).default;
    
    const enKeys = getAllKeys(enCommon).sort();
    const esKeys = getAllKeys(esCommon).sort();
    
    expect(enKeys).toEqual(esKeys);
  });

  it("en and es pages.json have the same key set", async () => {
    const enPages = (await import("@/shared/i18n/locales/en/pages.json")).default;
    const esPages = (await import("@/shared/i18n/locales/es/pages.json")).default;
    
    const enKeys = getAllKeys(enPages).sort();
    const esKeys = getAllKeys(esPages).sort();
    
    expect(enKeys).toEqual(esKeys);
  });

  it("renders admin/clusters page in Spanish", async () => {
    const i18nModule = await import("@/shared/i18n");
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.t("pages:adminClustersList.pageTitle")).toBe("Clústeres");
    expect(i18nModule.default.t("pages:adminClustersList.addClusterButton")).toBe("+ Agregar clúster");
    expect(i18nModule.default.t("pages:adminClustersList.healthy")).toBe("Saludable");
  });

  it("renders admin/policies page in Spanish", async () => {
    const i18nModule = await import("@/shared/i18n");
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.t("pages:adminPolicies.pageTitle")).toBe("Políticas");
    expect(i18nModule.default.t("pages:adminPolicies.rolesSectionTitle")).toBe("Roles");
    expect(i18nModule.default.t("pages:adminPolicies.addCustomRoleButton")).toBe("+ Agregar rol personalizado");
  });

  it("renders admin/system page in Spanish", async () => {
    const i18nModule = await import("@/shared/i18n");
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.t("pages:adminSystem.pageTitle")).toBe("Configuración del sistema");
    expect(i18nModule.default.t("pages:adminSystem.saveChangesButton")).toBe("Guardar cambios");
  });

  it("renders auth/login page in Spanish", async () => {
    const i18nModule = await import("@/shared/i18n");
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.t("pages:authLogin.signInTitle")).toBe("Iniciar sesión en basement");
    expect(i18nModule.default.t("pages:authLogin.usernameLabel")).toBe("Nombre de usuario");
  });

  it("renders first-run wizard in Spanish", async () => {
    const i18nModule = await import("@/shared/i18n");
    await i18nModule.default.changeLanguage("es");
    
    expect(i18nModule.default.t("pages:adminFirstRun.welcomeTitle")).toBe("¡Bienvenido a basement!");
    expect(i18nModule.default.t("pages:adminFirstRun.clusterTitle")).toBe("Agregue su primer clúster");
  });
});
