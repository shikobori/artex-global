"use client";

import * as React from "react";

import { setClientCookie } from "@/lib/cookie.client";
import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

import { isLocale, LOCALE_COOKIE, LOCALE_STORAGE_KEY, type Locale, resolveLocale } from "./config";

// The display language is a frontend-only preference stored in localStorage and a cookie,
// without server persistence or account synchronization. Changing browsers or clearing site data
// restores the default. Agent output language is stored separately in server settings.

function parseLocale(raw: string | null): Locale {
  return isLocale(raw) ? raw : resolveLocale();
}

// Subscribers in this tab. The storage event fires only in other tabs, so emit must notify
// local subscribers after a language change to rerender immediately.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

function emit() {
  for (const listener of listeners) listener();
}

function getSnapshot(): Locale {
  return parseLocale(getLocalStorageValue(LOCALE_STORAGE_KEY));
}

// The server has no localStorage; render the build default first, then let getSnapshot
// apply the user selection after hydration.
function getServerSnapshot(): Locale {
  return resolveLocale();
}

// Subscribe to the active display language and rerender consumers when it changes.
export function useActiveLocale(): Locale {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

// Change the display language, write the cookie/localStorage, and notify same-tab subscribers.
// Keep the cookie for one year so the selection persists on future visits.
export function setActiveLocale(locale: Locale) {
  if (!isLocale(locale)) return;
  setLocalStorageValue(LOCALE_STORAGE_KEY, locale);
  setClientCookie(LOCALE_COOKIE, locale, 365);
  emit();
}
