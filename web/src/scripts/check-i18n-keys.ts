/**
 * Script: check-i18n-keys.ts
 *
 * Locale message key consistency guard for web/messages/{ko,en,zh,es,ja}.json.
 * Use ko.json as the baseline and check every other locale for both missing and extra keys.
 * Exit nonzero on mismatch. Run with npm run check:i18n.
 */

import { diffKeys, flattenKeys, isInSync } from "./i18n-keys";
import fs from "node:fs";
import path from "node:path";

const MESSAGES_DIR = path.resolve(__dirname, "../../messages");
const BASELINE = "ko";
const LOCALES = ["en", "zh", "es", "ja"] as const;

function readLocale(locale: string): Set<string> {
  const file = path.join(MESSAGES_DIR, `${locale}.json`);
  const tree = JSON.parse(fs.readFileSync(file, "utf8")) as unknown;
  return flattenKeys(tree);
}

function preview(keys: string[]): string {
  const shown = keys.slice(0, 20).join(", ");
  return keys.length > 20 ? `${shown} …` : shown;
}

const baseline = readLocale(BASELINE);
let failed = false;

console.log("─".repeat(64));
console.log(`i18n key consistency check (baseline: ${BASELINE}.json, ${baseline.size} keys)`);
console.log("─".repeat(64));

for (const locale of LOCALES) {
  const diff = diffKeys(baseline, readLocale(locale));
  if (isInSync(diff)) {
    console.log(`[OK] ${locale}.json — match`);
    continue;
  }
  failed = true;
  console.log(`[FAIL] ${locale}.json — mismatch`);
  if (diff.missing.length) console.log(`   Missing ${diff.missing.length}: ${preview(diff.missing)}`);
  if (diff.extra.length) console.log(`   Extra ${diff.extra.length}: ${preview(diff.extra)}`);
}

console.log("─".repeat(64));
if (failed) {
  console.error("i18n keys do not match. Fix missing/extra keys and run again.");
  process.exit(1);
}
console.log("All locale key sets match.");
