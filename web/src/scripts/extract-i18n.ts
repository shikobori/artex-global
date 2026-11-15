/**
 * Script: extract-i18n.ts
 *
 * ARTEX Korean localization: initial UI string extractor.
 *
 * Parse all .ts/.tsx files under src/ with the TypeScript AST and extract only hardcoded
 * Chinese strings exposed to users. Three kinds of nodes are captured:
 *   1) String literals, such as a Chinese dashboard title.
 *   2) Template literals, replacing embedded expressions with {var0}, {var1}, and so on.
 *   3) JSX text nodes, such as a Chinese label inside a span.
 * Comments (line or block) are not AST nodes and are therefore excluded automatically.
 * Chinese text in leading or trailing comments is outside this extractor's scope.
 *
 * Outputs in web/messages/:
 *   - zh.json: namespaced { key: "original Chinese text" }, retained for upstream comparison.
 *   - ko.json: matching structure with empty values, filled with Korean translations in B3.
 *   - zh.sources.json: source file/line, kind, and placeholders per key for B2 wiring, B3 translation, and drift tracking.
 *
 * Re-extraction is nondestructive. Refresh only generated top-level namespaces derived from paths,
 * such as app, components, and lib. Preserve other curated namespaces already in the files,
 * such as manually translated nav, search, and header. Generated namespaces are inventories:
 * do not edit them manually. Keep runtime translations in curated namespaces so extraction
 * cannot overwrite them.
 *
 * Derive namespaces from paths, for example src/app/(main)/system/llm/page.tsx
 *   becomes app.main.system.llm.page (strip route-group parentheses and leading folder underscores).
 * Keys use the first eight SHA-1 characters of the source text. Identical text keeps the same key
 * despite reordering or file moves, minimizing diffs for upstream comparison.
 *
 * Run: npm run extract:i18n
 */

import * as ts from "typescript";

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

const SRC_DIR = path.resolve(__dirname, "..");
const MESSAGES_DIR = path.resolve(__dirname, "../../messages");
const REPO_WEB_DIR = path.resolve(__dirname, "../..");

/** Whether text contains at least one Han character, the sole criterion for UI extraction. */
const HAN = /\p{Script=Han}/u;

/** Directories excluded from extraction: this parser and build outputs. */
const SKIP_DIRS = new Set(["scripts", "node_modules", ".next"]);

type Kind = "string" | "template" | "jsx";

interface Message {
  ns: string;
  key: string;
  text: string;
  kind: Kind;
  placeholders: string[]; // Original template ${...} expressions for reference.
  occurrences: string[]; // "relative/path:line"
}

/** Convert a path relative to src into a dot-separated namespace. */
function toNamespace(absFile: string): string {
  const rel = path.relative(SRC_DIR, absFile).replace(/\.[tj]sx?$/, "");
  return rel
    .split(path.sep)
    .map((seg) =>
      seg
        .replace(/^\((.*)\)$/, "$1") // (main) -> main (route group)
        .replace(/^_/, "") // _components → components
        .replace(/[^A-Za-z0-9가-힣]+/g, "-")
        .replace(/^-+|-+$/g, ""),
    )
    .filter(Boolean)
    .join(".");
}

function hashKey(text: string): string {
  return crypto.createHash("sha1").update(text).digest("hex").slice(0, 8);
}

/** Recursively collect .ts/.tsx files. */
function collectFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      collectFiles(path.join(dir, entry.name), out);
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      out.push(path.join(dir, entry.name));
    }
  }
  return out;
}

/** Rebuild templates as literal segments plus {varN}; return null when literal segments contain no Han characters. */
function reconstructTemplate(node: ts.TemplateExpression): { text: string; placeholders: string[] } | null {
  let text = node.head.text;
  const placeholders: string[] = [];
  node.templateSpans.forEach((span, i) => {
    placeholders.push(span.expression.getText());
    text += `{var${i}}${span.literal.text}`;
  });
  // Han characters only inside ${} expressions are captured separately as inner nodes.
  if (!HAN.test(node.head.text) && !node.templateSpans.some((s) => HAN.test(s.literal.text))) {
    return null;
  }
  return { text, placeholders };
}

/** Normalize JSX text: trim edges and collapse internal whitespace/newlines to one space, matching browser rendering. */
function normalizeJsxText(raw: string): string {
  return raw.replace(/\s+/g, " ").trim();
}

const messages = new Map<string, Message>(); // "ns\u0000key" → Message
const scannedFiles = collectFiles(SRC_DIR).sort();
const capturedLines = new Map<string, Set<number>>(); // File to set of line numbers covered by extracted nodes.
let fileWithHan = 0;

function record(ns: string, text: string, kind: Kind, placeholders: string[], relFile: string, line: number) {
  const key = hashKey(text);
  const id = `${ns}\u0000${key}`;
  let msg = messages.get(id);
  if (!msg) {
    msg = { ns, key, text, kind, placeholders, occurrences: [] };
    messages.set(id, msg);
  }
  const occ = `${relFile}:${line}`;
  if (!msg.occurrences.includes(occ)) msg.occurrences.push(occ);
}

for (const absFile of scannedFiles) {
  const source = fs.readFileSync(absFile, "utf8");
  if (!HAN.test(source)) continue;
  fileWithHan++;

  const relFile = path.relative(REPO_WEB_DIR, absFile);
  const ns = toNamespace(absFile);
  const scriptKind = absFile.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sf = ts.createSourceFile(absFile, source, ts.ScriptTarget.Latest, true, scriptKind);
  const lineOf = (pos: number) => sf.getLineAndCharacterOfPosition(pos).line + 1;
  const markLine = (pos: number) => {
    const set = capturedLines.get(relFile) ?? new Set<number>();
    set.add(lineOf(pos));
    capturedLines.set(relFile, set);
  };

  const visit = (node: ts.Node): void => {
    if (ts.isStringLiteralLike(node) && !ts.isTemplateExpression(node.parent)) {
      // String literals and substitution-free templates; interpolated template heads/middles are handled elsewhere.
      if (HAN.test(node.text)) {
        record(ns, node.text, "string", [], relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    } else if (ts.isTemplateExpression(node)) {
      const r = reconstructTemplate(node);
      if (r) {
        record(ns, r.text, "template", r.placeholders, relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    } else if (ts.isJsxText(node)) {
      const text = normalizeJsxText(node.text);
      if (text && HAN.test(text)) {
        record(ns, text, "jsx", [], relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// Build nested JSON
type Tree = { [k: string]: Tree | string };

function setNested(root: Tree, nsPath: string, key: string, value: string) {
  const segs = nsPath.split(".");
  let node: Tree = root;
  for (const seg of segs) {
    if (typeof node[seg] !== "object") node[seg] = {};
    node = node[seg] as Tree;
  }
  node[key] = value;
}

/** Recursively sort keys to minimize diffs on repeated runs. */
function sortTree(t: Tree): Tree {
  const out: Tree = {};
  for (const k of Object.keys(t).sort()) {
    const v = t[k];
    out[k] = typeof v === "string" ? v : sortTree(v);
  }
  return out;
}

const zhTree: Tree = {};
const koTree: Tree = {};
const sources: Record<string, { text: string; kind: Kind; placeholders: string[]; occurrences: string[] }> = {};

for (const msg of messages.values()) {
  setNested(zhTree, msg.ns, msg.key, msg.text);
  setNested(koTree, msg.ns, msg.key, "");
  sources[`${msg.ns}.${msg.key}`] = {
    text: msg.text,
    kind: msg.kind,
    placeholders: msg.placeholders,
    occurrences: msg.occurrences.sort(),
  };
}

const sortedSources: typeof sources = {};
for (const k of Object.keys(sources).sort()) sortedSources[k] = sources[k];

fs.mkdirSync(MESSAGES_DIR, { recursive: true });
const write = (name: string, data: unknown) =>
  fs.writeFileSync(path.join(MESSAGES_DIR, name), `${JSON.stringify(data, null, 2)}\n`, "utf8");

/** Read an existing message file, returning an empty tree if absent or invalid. */
function readTree(name: string): Tree {
  try {
    return JSON.parse(fs.readFileSync(path.join(MESSAGES_DIR, name), "utf8")) as Tree;
  } catch {
    return {};
  }
}

/**
 * Nondestructive merge: code is authoritative for top-level namespaces in the newly extracted tree (fresh).
 * Preserve top-level namespaces found only in existing files, which contain manually curated translations.
 * Obsolete keys within generated namespaces disappear naturally when removed upstream,
 * making upstream comparisons easier.
 */
function mergeCurated(fresh: Tree, existing: Tree): Tree {
  const extractedTop = new Set(Object.keys(fresh));
  const out: Tree = { ...fresh };
  for (const [ns, subtree] of Object.entries(existing)) {
    if (!extractedTop.has(ns)) out[ns] = subtree;
  }
  return out;
}

write("zh.json", sortTree(mergeCurated(zhTree, readTree("zh.json"))));
write("ko.json", sortTree(mergeCurated(koTree, readTree("ko.json"))));
write("zh.sources.json", sortedSources);

// Summary report
const byKind: Record<Kind, number> = { string: 0, template: 0, jsx: 0 };
const byNs = new Map<string, number>();
let totalOccurrences = 0;
for (const msg of messages.values()) {
  byKind[msg.kind]++;
  byNs.set(msg.ns, (byNs.get(msg.ns) ?? 0) + 1);
  totalOccurrences += msg.occurrences.length;
}

// Estimate coverage as the fraction of source lines containing Han characters covered by extracted nodes.
// Most remaining lines are comments, outside this extractor's translation scope.
let hanLines = 0;
let capturedHanLines = 0;
for (const absFile of scannedFiles) {
  const relFile = path.relative(REPO_WEB_DIR, absFile);
  const lines = fs.readFileSync(absFile, "utf8").split(/\r?\n/);
  const capSet = capturedLines.get(relFile) ?? new Set<number>();
  lines.forEach((ln, i) => {
    if (HAN.test(ln)) {
      hanLines++;
      if (capSet.has(i + 1)) capturedHanLines++;
    }
  });
}

const topNs = [...byNs.entries()].sort((a, b) => b[1] - a[1]).slice(0, 15);

console.log("─".repeat(64));
console.log("ARTEX i18n extraction complete");
console.log("─".repeat(64));
console.log(`Scanned files         : ${scannedFiles.length} (.ts/.tsx under src/, excluding scripts)`);
console.log(`Files containing Han  : ${fileWithHan}`);
console.log(`Unique messages (keys): ${messages.size}`);
console.log(`  ├─ String literals   : ${byKind.string}`);
console.log(`  ├─ Template literals : ${byKind.template}`);
console.log(`  └─ JSX text          : ${byKind.jsx}`);
console.log(`Total occurrences     : ${totalOccurrences} (including repeated uses)`);
console.log(`Namespaces            : ${byNs.size}`);
console.log("");
console.log(`Source lines with Han : ${hanLines}`);
console.log(`  └─ Captured lines    : ${capturedHanLines} (${((capturedHanLines / hanLines) * 100).toFixed(1)}%)`);
console.log(
  `     The remaining ${hanLines - capturedHanLines} lines are mostly code comments (outside extraction scope)`,
);
console.log("");
console.log("Top 15 namespaces by message count:");
for (const [ns, n] of topNs) console.log(`  ${String(n).padStart(4)}  ${ns}`);
console.log("");
console.log(`Output: web/messages/{zh.json, ko.json, zh.sources.json}`);
console.log("─".repeat(64));
