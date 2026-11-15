import { getRequestConfig } from "next-intl/server";

import { type Locale, resolveLocale } from "./config";

// Explicit locale loader imports prevent the bundler from including the entire messages/ directory
// as a context module, excluding the local-only zh.sources.json file.
const loaders: Record<Locale, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import("../../messages/en.json"),
  ko: () => import("../../messages/ko.json"),
  zh: () => import("../../messages/zh.json"),
  es: () => import("../../messages/es.json"),
  ja: () => import("../../messages/ja.json"),
};

// next-intl request settings. Without locale routing segments or middleware, choose the locale here.
// Do not reference requestLocale, which would trigger dynamic rendering. These settings only
// provide the server-component/prerender default; IntlRuntimeProvider handles runtime user
// switching on the client using messages/en, ko, zh, es, and ja.
export default getRequestConfig(async () => {
  const locale = resolveLocale();
  const messages = (await loaders[locale]()).default;
  return { locale, messages };
});
