import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

export const SUPPORTED_LANGUAGES = ["en", "es"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

const fallbackLng: SupportedLanguage = "en";

i18next
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    lng: undefined,
    fallbackLng,
    supportedLngs: SUPPORTED_LANGUAGES,
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ["localStorage", "navigator"],
      caches: ["localStorage"],
      lookupLocalStorage: "basement_language",
    },
    react: {
      useSuspense: true,
    },
    defaultNS: "common",
    fallbackNS: "common",
    ns: ["common", "pages"],
  });

export default i18next;
