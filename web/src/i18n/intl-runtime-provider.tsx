"use client";

import type { ReactNode } from "react";
import * as React from "react";

import { NextIntlClientProvider } from "next-intl";

import enMessages from "../../messages/en.json";
import esMessages from "../../messages/es.json";
import jaMessages from "../../messages/ja.json";
import koMessages from "../../messages/ko.json";
import zhMessages from "../../messages/zh.json";
import type { Locale } from "./config";
import { useActiveLocale } from "./locale-store";

// Bundle all five locale message sets statically so switching languages in settings immediately
// rerenders all strings without rebuilding, refreshing, or asynchronous loading.
// This also supports static output: "export".
const MESSAGES: Record<Locale, Record<string, unknown>> = {
  en: enMessages as Record<string, unknown>,
  ko: koMessages as Record<string, unknown>,
  zh: zhMessages as Record<string, unknown>,
  es: esMessages as Record<string, unknown>,
  ja: jaMessages as Record<string, unknown>,
};

// Time zone of the runtime environment: browser or prerender server.
const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;

// Provider for runtime locale switching. Replace the root layout NextIntlClientProvider
// with messages selected by the active user locale stored in cookies/localStorage.
export function IntlRuntimeProvider({ children }: Readonly<{ children: ReactNode }>) {
  const locale = useActiveLocale();

  // Update <html lang> on language changes, overriding the initial server-rendered value
  // so screen readers and browser translation use the selected language.
  React.useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  // Pass timeZone explicitly because this provider does not inherit server request settings
  // like the root layout provider; omitting it causes next-intl ENVIRONMENT_FALLBACK errors during prerendering.
  return (
    <NextIntlClientProvider locale={locale} messages={MESSAGES[locale]} timeZone={TIME_ZONE}>
      {children}
    </NextIntlClientProvider>
  );
}
