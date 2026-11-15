/**
 * Locale message key consistency helpers.
 *
 * Compare each locale's flattened key set against ko.json as the baseline to find both missing
 * and extra keys. The CLI (check-i18n-keys.ts) and Vitest tests share this pure logic,
 * keeping the validation rules in one place.
 */

export interface KeyDiff {
  missing: string[]; // Keys present in baseline but absent from target.
  extra: string[]; // Keys absent from baseline but present in target.
}

/** Flatten nested JSON into "a.b.c" keys. Treat arrays and primitives as leaves. */
export function flattenKeys(tree: unknown, prefix = ""): Set<string> {
  const out = new Set<string>();
  if (tree === null || typeof tree !== "object" || Array.isArray(tree)) {
    if (prefix) out.add(prefix);
    return out;
  }
  for (const [key, value] of Object.entries(tree as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${key}` : key;
    for (const leaf of flattenKeys(value, path)) out.add(leaf);
  }
  return out;
}

/** Return sorted arrays of missing and extra keys in target relative to baseline. */
export function diffKeys(baseline: Set<string>, target: Set<string>): KeyDiff {
  const missing = [...baseline].filter((k) => !target.has(k)).sort();
  const extra = [...target].filter((k) => !baseline.has(k)).sort();
  return { missing, extra };
}

/** True when both missing and extra are empty. */
export function isInSync(diff: KeyDiff): boolean {
  return diff.missing.length === 0 && diff.extra.length === 0;
}
