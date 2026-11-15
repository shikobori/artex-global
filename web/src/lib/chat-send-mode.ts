"use client";

import * as React from "react";

import { useTranslations } from "next-intl";

import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

// Send/newline shortcuts for the session input are frontend-only preferences in localStorage,
// without server persistence or account synchronization, so changing browsers requires setting them again.
// See issue #39: version 0.3.2 changed submission from Ctrl+Enter to Enter;
// this restores the previous shortcut as an option.
export type ChatSendMode = "enter" | "ctrl-enter";

export const CHAT_SEND_MODE_KEY = "artex_chat_send_mode";
export const DEFAULT_CHAT_SEND_MODE: ChatSendMode = "enter";

// Options for the send-mode dropdown in settings. Return them through a hook because labels
// depend on locale; hardcoding Korean in a pure module would break the Chinese locale.
// Values are stable identifiers used unchanged for localStorage and behavior checks.
export function useChatSendModeOptions(): { value: ChatSendMode; label: string }[] {
  const t = useTranslations("chatSendMode.option");
  return [
    { value: "enter", label: t("enter") },
    { value: "ctrl-enter", label: t("ctrlEnter") },
  ];
}

function parseMode(raw: string | null): ChatSendMode {
  return raw === "ctrl-enter" || raw === "enter" ? raw : DEFAULT_CHAT_SEND_MODE;
}

// Subscribers in the current tab. localStorage storage events fire only in other tabs,
// so emit must notify same-tab inputs after changes or they would require a refresh
// to pick up the new setting.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

// Return a string literal so Object.is compares by value and useSyncExternalStore
// does not enter an infinite loop.
function getSnapshot(): ChatSendMode {
  return parseMode(getLocalStorageValue(CHAT_SEND_MODE_KEY));
}

// The server has no localStorage; render the default first, then let getSnapshot
// correct it after hydration.
function getServerSnapshot(): ChatSendMode {
  return DEFAULT_CHAT_SEND_MODE;
}

export function useChatSendMode(): ChatSendMode {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function setChatSendMode(mode: ChatSendMode) {
  setLocalStorageValue(CHAT_SEND_MODE_KEY, mode);
  for (const listener of listeners) listener();
}

// shouldSubmitOnKey checks whether a keystroke submits the message.
// isComposing / keyCode 229 indicate active IME composition, such as Korean or Chinese, and must pass through
// to avoid submitting when Enter merely confirms a character.
// The enter mode excludes only Shift to preserve version 0.3.2 behavior exactly for users
// who leave the setting unchanged. ctrl-enter accepts both Ctrl and Cmd on macOS.
export function shouldSubmitOnKey(e: React.KeyboardEvent, mode: ChatSendMode): boolean {
  if (e.key !== "Enter") return false;
  if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return false;
  if (mode === "ctrl-enter") return e.ctrlKey || e.metaKey;
  return !e.shiftKey;
}
