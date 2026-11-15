// Supported locales and defaults. artex-global defaults to English and offers Korean, Chinese, Spanish,
// and Japanese in user settings. Chinese ("zh") also supports comparison with the upstream source.
export const LOCALES = ["en", "ko", "zh", "es", "ja"] as const;
export type Locale = (typeof LOCALES)[number];

// English is the default display language; users can choose Korean, Chinese, Spanish, or Japanese.
// NEXT_PUBLIC_LOCALE overrides the build default; the runtime selection in cookies/localStorage takes precedence.
export const DEFAULT_LOCALE: Locale = "en";

// Native language names for the locale selector. Keep these fixed rather than translating them
// so users can recognize their own language regardless of the active display language.
export const LOCALE_LABELS: Record<Locale, string> = {
  en: "English",
  ko: "한국어",
  zh: "中文",
  es: "Español",
  ja: "日本語",
};

// Storage key for the selected display language. The UI reads localStorage; the same value is
// also written to a cookie for future server-side locale detection, but currently no code reads it.
export const LOCALE_COOKIE = "artex_locale";
export const LOCALE_STORAGE_KEY = "artex_locale";

// Type guard that narrows arbitrary strings to supported locales.
export function isLocale(raw: unknown): raw is Locale {
  return typeof raw === "string" && (LOCALES as readonly string[]).includes(raw);
}

// Choose the active locale at build time. To support static export (output: "export" in next.config),
// read only environment variables, without dynamic APIs such as cookies() or headers().
// Use the default if NEXT_PUBLIC_LOCALE is empty or unsupported. This value is only for the initial
// server-prerendered HTML; the client overrides it with the user-selected display language.
export function resolveLocale(): Locale {
  const raw = process.env.NEXT_PUBLIC_LOCALE;
  return isLocale(raw) ? raw : DEFAULT_LOCALE;
}
