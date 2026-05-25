import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

import enCommon from "./locales/en/common.json";
import enPages from "./locales/en/pages.json";
import esCommon from "./locales/es/common.json";
import esPages from "./locales/es/pages.json";

// SUPPORTED_LANGUAGES is the typed array of locales the UI ships
// translations for. v2.0.0-beta.3 ships en + es as anchors; v2.0.0-beta.5
// will grow this to ~20-30 LTR locales (operator scope: any language
// translatable at ≥97%, LTR only — explicitly no Arabic, Hebrew, etc.).
export const SUPPORTED_LANGUAGES = ["en", "es"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

// Resources imported statically — first paint is synchronous and
// useTranslation never suspends. Bundle cost is tiny (~2-4 KB per
// locale) and avoids the i18n-suspends-test-renders class of bug
// caught in v2.0.0-beta.3.1.
const resources = {
  en: { common: enCommon, pages: enPages },
  es: { common: esCommon, pages: esPages },
};

const fallbackLng: SupportedLanguage = "en";

i18next
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng,
    supportedLngs: SUPPORTED_LANGUAGES,
    // v1.13.34: collapse regional variants to the base language so
    // navigator="es-MX" / "es-AR" / "es-ES" all map to "es", and
    // "en-US" / "en-GB" / "en-CA" to "en". Without this, region-
    // tagged browsers fell through to fallbackLng even when the base
    // language IS supported — the operator's flag for "language
    // should detect from system or default to english if not found".
    load: "languageOnly",
    nonExplicitSupportedLngs: true,
    interpolation: {
      escapeValue: false,
    },
    detection: {
      // Order: 1) explicit user choice in localStorage (sticky across
      // sessions); 2) navigator.language(s) (system preference); 3)
      // fallbackLng kicks in if neither yields a supported match.
      order: ["localStorage", "navigator"],
      caches: ["localStorage"],
      lookupLocalStorage: "basement_language",
    },
    react: {
      useSuspense: false,
    },
    defaultNS: "common",
    fallbackNS: "common",
    ns: ["common", "pages"],
  });

export default i18next;
