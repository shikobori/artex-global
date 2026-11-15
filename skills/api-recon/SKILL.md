---
name: api-recon
description: Use this skill to collect website API endpoints.
---

# API Recon (Frontend API Reconnaissance)

With **authorization**, discover as comprehensively as possible: **backend APIs** (paths, methods, parameters, response bodies), **frontend routes**, and **UI feature triggers** (tabs, dialogs, table actions, etc.).

---

## Boundaries and Prohibitions (Required Reading for Agents)

This skill covers **API / parameter surface reconnaissance only**, not vulnerability discovery or exploitation.

### Task boundaries

| Area | Allowed | Prohibited |
|---|---|---|
| **Targets** | Enumerate paths, methods, parameters, routes, and UI triggers | SQLi/XSS/access-control/brute-force/fuzz vulnerability testing, request-tampering attacks, destructive operations |
| **Authentication** | Hooks + stubs/mocks to bypass **client-side** login gates | Requesting or guessing usernames/passwords; submitting real login forms |
| **Runtime** | Hook APIs without credentials and use mock responses to render the SPA's authenticated shell | Workflows that require a real backend session to continue |

### Dynamic analysis without credentials (Phase 3 default)

1. **Intercept and stub** login, permission, menu, and other bootstrap APIs using `preload.js` / `runtime_harvest.js`;
2. Return mock bodies for business queries with **valid structure, successful application status codes, and optionally empty data**;
3. Let the frontend render authenticated pages even without a backend or under 401 responses, triggering more XHR/fetch/WebSocket requests;
4. **Empty data, blank tables, and placeholder UI are expected**—do not switch to real login or vulnerability testing to resolve them.

**In brief**: use mocks to expose frontend routes and mount components; **record outbound requests only**. The backend response is not the objective; discover **which additional APIs the frontend calls**.

### Strict workflow prohibitions

| Prohibited | Alternative |
|---|---|
| Using grep/curl/Read on the main `index-*.js` entry to extract API paths before Phase 1 is complete | Run `OUTDIR/harvest_static.py` |
| Writing `extract_apis.py` or similar scripts to replace harvest | Modify `OUTDIR/harvest_static.py` and rerun |
| Repeating the same grep/command after ≥2 failures | Change strategy: read tool_logs, modify harvest, consult the reference |
| Skipping gates A/B and running the original `scripts/` files directly | Copy to OUTDIR and adapt to the target |
| Real username/password, OTP, OAuth, or other authentication | Use stubs/mocks (see above) |
| Skipping stubs to obtain real data through access-control or injection tests | Record outbound requests only, within recon scope |
| Irreversible operations such as deletion, sensitive-data export, or bulk writes | The same restriction applies to coverage clicks |
| Claiming all pages and APIs were discovered without completing runtime analysis and dynamic enumeration | See the definition of completion or state the limitations |
| Claiming all parameters are known without completing the parameter trigger matrix and diff | Complete the Phase 3b matrix and Phase 5 diff |
| Inferring required/optional fields from a single runtime sample | Compare multiple samples or infer from validation rules/errors |

---

## Two-layer Model and Runtime Modes

| Layer | Output | Limitations |
|---|---|---|
| **Static** (JS bundle) | All endpoint paths, a route draft, candidate fields at payload construction sites | No HTTP methods; parameters require Phase 1b; misses URLs assembled at runtime |
| **Runtime** (live session) | Methods + bodies + responses + dynamic URLs + WS/SSE; multi-sample diffs complete parameters | Pages must actually render to send requests; one sample cannot establish required/optional fields |

| Runtime mode | Engine | Use cases |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response bodies, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Click tabs/dialogs/tables for deeper feature coverage |
| **both** | depth followed by coverage | Most complete; takes the longest |

**Parameter methodology** (no universal script): use harvest/regex for paths; use **expanded anchor windows + UI binding chains + multi-sample diffs + error-based inference** for parameters (grep recipes in [reference.md](reference.md), section J).

---

## Definition of Completion

All conditions must be met before claiming recon is complete:

- [ ] **Static**: Phase 1 harvest produces `api_static.txt`, `routes.txt`, and `js/`
- [ ] **Runtime**: at least depth or coverage; coverage/both requires **working hooks + a dynamic enumeration loop**
- [ ] **Shell access**: business paths do not resolve to `/login` (watch hash routing)
- [ ] **Parameters**: coverage/both completes the parameter trigger matrix + `param_samples.json`; Phase 5 merges `params_merged.json`
- [ ] **Depth** (if module pages are blank): reconstruct the Phase 4 permission tree and rerun until **module-level APIs** appear, beyond locale/bootstrap
- [ ] **Delivery**: all Phase 5 outputs exist (see its output table); `insert_assets` writes service and endpoint assets

---

## Scripts and Gates

`scripts/` contains reference templates only. **Do not** run the originals directly and treat their output as final.

**Rule**: read → adapt to the target → write to `OUTDIR` (e.g. `recon/`) → record `CHANGES.md`. If unsuitable, rewrite using the methodology and borrow only the structure.

| Gate | When | Reference script → OUTDIR copy | Common required adaptations |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **Default regex works for most sites**; change endpoint regex, webpack/Vite `publicPath`, or MPA exclude/cookie only when the manifest/dialect differs |
| **B (runtime)** | After Phase 2, before depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success values, stubs, login regex, API prefixes, hash/history |

**Mandatory SPA order** (cannot be rearranged; phase numbers take precedence over exploring before scripting):

| Step | Required | Prohibited |
|---|---|---|
| After Phase 0 | Next Bash command = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read on the main `index-*.js` entry (usually >500KB) |
| Gate A | Copy script → make small changes as needed → **run immediately** | Manually extracting APIs before deciding whether to harvest |
| Before Phase 1 completes | Verify outputs with `wc -l`; modify harvest and retry on 404 | Handwritten extraction scripts; repeatedly grepping URLs that have not been downloaded |
| From Phase 1b onward | grep only `OUTDIR/js/*.js` | Substituting the main bundle for harvest |

- ✅ Copy `harvest_static.py` → optionally change regex → **run immediately**
- ❌ curl the main bundle → grep repeatedly → write temporary extraction code → harvest last
- **MPA**: next Bash command after Phase 0 = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and Output Constraints

| Constraint | Explanation |
|---|---|
| Large files | **Do not** load `index-*.js` files >100KB into context with Read/grep; batch-process using OUTDIR scripts |
| grep output | Must use `\| head -20` or `-m 5`; keep only path summaries in conversation, without bundle excerpts |
| Verification | Use `wc -l` and `ls \| wc -l`; do not Read an entire directory |
| Initial regex exploration | Optional, ≤1 attempt, only a small chunk or HTML ≤50KB; harvest is authoritative for static analysis |
| Reference | Recipes/templates/troubleshooting are in [reference.md](reference.md); do not duplicate the full text inline |

---

## Execution Roadmap

```
Phase 0 classification + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b parameter reverse engineering
  → Phase 2 three authentication gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree (if needed) → rerun Phase 3
  → Phase 5 merged report + insert_assets: batch-insert all discovered service and API endpoint assets; never omit a discovered asset
```

Check off steps in order; **do not proceed to the next phase before completing the previous item**.

1. [ ] **Phase 0**: identify SPA/MPA; create `OUTDIR` → [Phase 0](#phase-0--classification)
2. [ ] **Gate A + Phase 1**: copy scripts → harvest **immediately** → verify with `wc -l` → [Phase 1](#phase-1--static-analysis)
3. [ ] **Phase 1b**: expanded anchor windows + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b--parameter-reverse-engineering)
4. [ ] **Phase 2**: three authentication gates → `config.json` → [Phase 2](#phase-2--three-authentication-gates)
5. [ ] **Gate B**: adapt runtime scripts → [Phase 3](#phase-3--runtime)
6. [ ] **Phase 3**: depth / coverage / both; verify shell access; parameter trigger matrix → `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4--permission-tree-reconstruction)
8. [ ] **Phase 5**: merge outputs + report + `insert_assets` → [Phase 5](#phase-5--merging-and-reporting)

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (do not modify the skill's `scripts/`):

- **SPA**: empty shell + `<div id=app>` + chunks → Phases 1–5
- **MPA**: SSR + `<form>`, no endpoint bundle → after gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Produces `forms.txt`, `links.txt`, and `api_inline.txt`. For an SPA with forms ≈ 0, switch to Phase 1.

---

## Phase 1 — Static Analysis

Follow [Scripts and Gates](#scripts-and-gates) and [Tool and Output Constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse HTML scripts → webpack/Vite manifest → download all lazy chunks → produce `js/`, `api_static.txt`, `routes.txt`, and `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Compare chunk counts against the manifest: on 404, modify harvest and retry; do not manually curl individual chunks
- Too few entries in `api_static.txt` → broaden the endpoint regex in OUTDIR and rerun (see reference)

### Phase 1b — Parameter Reverse Engineering

Paths come from Phase 1; parameter fields require separate recon. See [Tool and Output Constraints](#tool-and-output-constraints) for grep rules.

**Completion criteria**: for important endpoints, identify field names, transport locations, inferred types, whether required, sample values, and confidence.

#### 1b.0 — Transport Formats

| Format | Parameter location | Static inspection priority |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` beside the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route table + `useParams` / `$route.params` |
| Encryption/signing | Wrapped in `sign`/`data` | Hook encryption function inputs (reference section D) |

Output: annotate each endpoint with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Expanded Anchor Windows

Use a known path as an anchor and expand the window to find payload construction objects:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Shared request wrapper | Global fields injected by interceptors |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Second hook argument |
| Vue composable | Composable inputs |

Remaining type information: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`：`{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding Layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Technique |
|---|---|
| Form submit | Trace submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Route | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

Trace up the DevTools call stack from `fetch`/`XHR.send` to the payload construction function.

#### 1b.3 — Three Payload Questions (Distinct from Phase 2 Authentication Gates)

| Question | What to establish |
|---|---|
| **Construction** | Where the payload is built and evidence of transforms |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

At the interceptor gate (Phase 2), also inspect globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Connecting to Phase 3

Candidate fields come from static analysis/the binding layer; **required/optional/conditional dependencies** need the Phase 3 parameter matrix + diffs + Phase 5 error-based inference.

---

## Phase 2 — Three Authentication Gates

grep `OUTDIR/js/` (with `head`) and write `config.json` (recipes in reference):

| Gate | Question | Keywords |
|---|---|---|
| **Rendering gate** | How is login status determined? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers a redirect to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat localStorage key names as credentials—verify using chunks/the request chain.

**Exit = gate B**: record conclusions in `config.json` and modify `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API Observation (Optional)

Use `preload.js` in OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Configuration | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Child APIs in responses |
| `observe.storageReads/cookieReads: true` | Information to fill in config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export after each coverage round: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, and `__API_RECON_OBSERVE__`.

---

## Phase 3 — Runtime

Gate B must be complete; follow [Boundaries and Prohibitions](#boundaries-and-prohibitions-required-reading-for-agents) and the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in reference).

### Hooks and Stubs (Shared by depth + coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact match | auth/permission/bootstrap stubs | Pass initial page authentication |
| L2 negative-result correction | All JSON responses | Not-logged-in codes → success |
| L3 fallback | `/api` etc. not matched by L1 | Empty success bodies to render UI |

- **depth**: fake auth + `forward` to modify application status codes + `stubs`; traverse `routes` (hash/history); produce `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or a userscript)

Verify that `window.__API_RECON_PRELOAD__` exists and business paths do not return to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b — Dynamic Coverage Enumeration (Required)

1. Main navigation/sidebar — click every item and wait 1–3s for network activity
2. Tabs — `role=tab`, `.ant-tabs-tab`
3. Tables — view/edit/details on the first row
4. Toolbars — export, filter, create (**avoid irreversible deletion**)
5. On entering each module — merge APIs/routes
6. SPA — controlled `pushState` for uncovered paths in `routes.txt` (prohibited for MPA)

**Parameter trigger matrix** (required): record each action type once per module and **diff multiple samples**:

| Action | Typical additional parameters |
|---|---|
| Initial list page | Pagination + default filters |
| Click search | keyword, filter |
| Advanced filtering | More optional fields |
| Create/edit | Complete entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Outbound bodies/headers remain real when responses are stubbed**—use the requests as evidence. Record → `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4 — Permission Tree Reconstruction

**Trigger**: blank module pages / only bootstrap requests (e.g. locale) on each route → content gate not passed.

| Symptom | Meaning |
|---|---|
| Shell renders successfully | Rendering + interceptor gates passed |
| Missing sidebar items/blank content after clicks | Incomplete stub shape or permission codes |
| Very few, identical APIs on every route | `v-if permission` did not pass |
| Far fewer routes in `routes.txt` than in the bundle | Complete them from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, and `perm_codes_all.txt`.

Stub checks: outer `response_code` matches the interceptor gate; flat codes align with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. For large SPAs, tune `waitUntil`, `routeTimeout`, and `perRouteMs` (reference sections A3/I).

---

## Phase 5 — Merging and Reporting

### Output Table

| File | Phase | Contents |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Static candidate parameter fields |
| `config.json` | 2 | Three gates + runtime configuration |
| `runtime_api.json` | 3a | Detailed depth recordings (including WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click logs, details |
| `route_map.json`, etc. | 4 | Intermediate permission-tree files (if performed) |
| `params_merged.json` | 5 | Merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, params, features, limitations |
| **insert_assets** | 5 | Write all service and endpoint assets to the asset store |

### 5b — Parameter Merging

Diff `param_samples.json`; **there is no universal merge script**. See reference J7 for confidence rules (high/medium/low/pending trigger).

### 5c — Error-based Inference

Within authorization, send incomplete requests to inspect 400 errors (**parameter recon, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Watch for `data` wrappers, `variables`, and pre-encryption `bizData`.

The report must state runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a `CHANGES.md` summary of deviations from the reference scripts.

Suggested `site_map.json` structure:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

See [reference.md](reference.md) for additional fields and grep recipes.

---

## General Notes

- **Framework independent**: the same approach applies to webpack/Vite/Angular lazy loading
- **Transports**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope
- **SSR**: client-side fetch can be recorded; RSC/Server Actions cannot be fully enumerated
- **Blind spots**: JSVMP, WASM, strict HMAC/mTLS validation → static analysis + stated limitations
- **Parameter blind spots**: conditional dependencies, hidden params, WASM payload construction → mark as "pending trigger" / "unreachable"
- **Static analysis is the safety net**: static endpoint enumeration remains possible when runtime analysis is blocked

---

## Additional Resources

- Grep recipes, `config.json` templates, troubleshooting, hooks, parameter reverse engineering in section J, and site_map templates: **[reference.md](reference.md)**
- See the [Scripts and Gates](#scripts-and-gates) table for reference script paths
