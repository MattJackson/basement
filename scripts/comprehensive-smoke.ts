#!/usr/bin/env node
// comprehensive-smoke.ts — full Playwright route + state coverage for a
// live basement deployment.
//
// Sibling of scripts/postdeploy-ui-smoke.ts. The existing smoke is a
// hand-curated spot-check of the routes that have regressed in the past
// (~70 checks). This script is the systematic counterpart: it visits
// every route in frontend/src/routes/, captures desktop + mobile
// screenshots, exercises every form's validation paths, and walks the
// modals/dialogs that mutate state.
//
// =========================================================================
// SAFETY GUARANTEES
// =========================================================================
//
// This script runs against PRODUCTION (basement.pq.io) by default. To
// avoid the obvious failure mode — overwriting or deleting matthew's
// real data — destructive coverage uses ONLY ephemeral resources
// tagged with the run's timestamp + a random nonce, so a cleanup
// failure leaves obvious garbage that's easy to find and reap by hand.
//
// Naming pattern: `smoke-{kind}-{timestamp}-{rand}` where kind is one
// of region, sa, federation, backup, webhook. Real-data names (`lsi`,
// `cheshire`, real OIDC identities, real federations) are NEVER
// touched. Every destructive op operates on its own freshly-created
// ephemeral target.
//
// Before+after counts of real resources are recorded and compared at
// end-of-run; a mismatch is a loud failure even if every check passed.
//
// The cleanup loop runs in a finally block — even if a check throws,
// cleanup still attempts to reap every ephemeral resource. Failures
// are logged with the resource ID so an operator can manually scrub.
//
// =========================================================================
// USAGE
// =========================================================================
//
//   node scripts/comprehensive-smoke.ts
//   BASE_URL=https://basement.example.com \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/comprehensive-smoke.ts
//
// Output:
//   - Screenshots: /tmp/basement-smoke-{ts}/{desktop,mobile}/{name}.png
//   - Console: green [ok] / red [FAIL] / yellow [skip] / [warn]
//   - Final summary with pass/fail/skip counts + bug report
//
// Exit codes:
//   0  all checks passed (warnings ok)
//   1  one or more checks failed (cleanup still ran)
//   2  bad invocation / setup error
//
// Existing smoke (postdeploy-ui-smoke.ts) is unaffected — both scripts
// can run independently. The pnpm wrapper exposes both:
//   pnpm smoke         → existing curated smoke
//   pnpm smoke:full    → this comprehensive walk

import type { Browser, BrowserContext, ConsoleMessage, Page, chromium as ChromiumApi } from "playwright";
import { existsSync, mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const FRONTEND_NODE_MODULES = resolve(__dirname, "..", "frontend", "node_modules");
const PLAYWRIGHT_INDEX = join(FRONTEND_NODE_MODULES, "playwright", "index.mjs");

if (!existsSync(PLAYWRIGHT_INDEX)) {
  process.stderr.write(
    `[FAIL] playwright not found at ${PLAYWRIGHT_INDEX}\n` +
      `       install with: pnpm -C frontend install\n` +
      `       and:           pnpm -C frontend exec playwright install chromium\n`,
  );
  process.exit(2);
}

const PLAYWRIGHT_ENTRY = pathToFileURL(PLAYWRIGHT_INDEX).href;
const { chromium } = (await import(PLAYWRIGHT_ENTRY)) as { chromium: typeof ChromiumApi };

// ---------- axe-core (a11y) — optional, dynamic import ----------
// Cycle v1.11.0.12 wires axe-core into the screenshot pass so every
// route gets a free a11y audit alongside its desktop screenshot.
// Soft-loaded: if @axe-core/playwright isn't installed, the script
// still runs — a11y checks are skipped with a warn line. Install
// with: pnpm -C frontend add -D @axe-core/playwright
const AXE_INDEX = join(FRONTEND_NODE_MODULES, "@axe-core", "playwright", "dist", "index.js");
type AxeBuilderCtor = new (opts: { page: Page }) => {
  analyze(): Promise<{
    violations: Array<{ id: string; impact?: string; description: string; nodes: unknown[] }>;
  }>;
};
let AxeBuilder: AxeBuilderCtor | undefined;
if (existsSync(AXE_INDEX)) {
  try {
    const mod = (await import(pathToFileURL(AXE_INDEX).href)) as {
      default?: AxeBuilderCtor;
      AxeBuilder?: AxeBuilderCtor;
    };
    AxeBuilder = mod.AxeBuilder ?? mod.default;
  } catch (e) {
    process.stderr.write(`[warn] axe-core load failed: ${e instanceof Error ? e.message : String(e)}\n`);
  }
}

// ---------- config ----------
const BASE_URL = (process.env.BASE_URL ?? "https://basement.pq.io").replace(/\/$/, "");
const USERNAME = process.env.BUI_USERNAME ?? process.env.BASEMENT_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? process.env.BASEMENT_PASSWORD ?? process.env.PASSWORD ?? "password";

const RUN_TS = new Date().toISOString().replace(/[:.]/g, "-");
const RUN_NONCE = Math.random().toString(36).slice(2, 8);
const SHOT_ROOT = process.env.SMOKE_SHOT_DIR ?? join("/tmp", `basement-smoke-${RUN_TS}`);
const SHOT_DESKTOP = join(SHOT_ROOT, "desktop");
const SHOT_MOBILE = join(SHOT_ROOT, "mobile");
mkdirSync(SHOT_DESKTOP, { recursive: true });
mkdirSync(SHOT_MOBILE, { recursive: true });

// Ephemeral resource tags. EVERY mutation that touches the server
// uses one of these prefixes so cleanup can find and reap them, and
// an operator scanning their backend can identify smoke leftovers at
// a glance.
const EPH_PREFIX = `smoke-${Date.now()}-${RUN_NONCE}`;
const ephName = (kind: string) => `${EPH_PREFIX}-${kind}`;

// ---------- color helpers ----------
const isTTY = process.stdout.isTTY;
const C = {
  green: isTTY ? "\x1b[32m" : "",
  red: isTTY ? "\x1b[31m" : "",
  yellow: isTTY ? "\x1b[33m" : "",
  dim: isTTY ? "\x1b[2m" : "",
  bold: isTTY ? "\x1b[1m" : "",
  reset: isTTY ? "\x1b[0m" : "",
};

function info(msg: string) {
  process.stdout.write(`${C.dim}${msg}${C.reset}\n`);
}
function section(msg: string) {
  process.stdout.write(`\n${C.bold}${msg}${C.reset}\n`);
}
function passLine(name: string, ms: number) {
  process.stdout.write(`${C.green}[ok]${C.reset} ${name} ${C.dim}(${(ms / 1000).toFixed(2)}s)${C.reset}\n`);
}
function failLine(name: string, detail?: string) {
  process.stderr.write(`${C.red}[FAIL]${C.reset} ${name}\n`);
  if (detail) process.stderr.write(`  ${C.dim}${detail}${C.reset}\n`);
}
function warnLine(msg: string) {
  process.stderr.write(`${C.yellow}[warn]${C.reset} ${msg}\n`);
}
function skipLine(name: string, reason: string) {
  process.stdout.write(`${C.yellow}[skip]${C.reset} ${name} ${C.dim}(${reason})${C.reset}\n`);
  results.push({ name, ok: true, skipped: true, ms: 0, detail: `skipped: ${reason}` });
}

// ---------- results / bug report ----------
type Result = { name: string; ok: boolean; skipped?: boolean; ms: number; detail?: string };
const results: Result[] = [];
const bugReport: string[] = [];

function reportBug(area: string, detail: string) {
  bugReport.push(`[${area}] ${detail}`);
}

async function check(name: string, fn: () => Promise<void>): Promise<boolean> {
  const start = Date.now();
  try {
    await fn();
    const ms = Date.now() - start;
    results.push({ name, ok: true, ms });
    passLine(name, ms);
    return true;
  } catch (err) {
    const ms = Date.now() - start;
    const detail = err instanceof Error ? err.message : String(err);
    results.push({ name, ok: false, ms, detail });
    failLine(name, detail);
    return false;
  }
}

// ---------- console / pageerror tracking ----------
type LoggedConsole = { type: string; text: string; url: string };
const consoleErrors: LoggedConsole[] = [];
const pageErrors: { message: string; url: string }[] = [];

function attachListeners(page: Page) {
  page.on("console", (msg: ConsoleMessage) => {
    const t = msg.type();
    const entry = { type: t, text: msg.text(), url: page.url() };
    if (t === "error") {
      // Filter low-signal browser failures we can't act on.
      if (entry.text.includes("Failed to load resource")) return;
      // Sentry/analytics noise filter — none deployed but reserved.
      consoleErrors.push(entry);
    }
  });
  page.on("pageerror", (err: Error) => {
    pageErrors.push({ message: `${err.name}: ${err.message}`, url: page.url() });
  });
}

// ---------- screenshot helpers ----------
// Cycle v1.11.0.12: shotDesktop now ALSO runs an axe-core a11y audit
// (if @axe-core/playwright is installed) and tags any violations
// into the bug report. Failures from axe never fail the smoke run —
// a11y is tracked over time, not gated on. See axeAudits[] for the
// rollup that lands in the final summary.
type AxeAudit = { route: string; routeUrl: string; violations: number; ids: string[] };
const axeAudits: AxeAudit[] = [];
async function runAxe(page: Page, name: string) {
  if (!AxeBuilder) return;
  try {
    const audit = await new AxeBuilder({ page }).analyze();
    if (audit.violations.length > 0) {
      const ids = audit.violations.map((v) => v.id);
      axeAudits.push({ route: name, routeUrl: page.url(), violations: audit.violations.length, ids });
      // Loud-but-non-failing — a11y is signal, not a gate.
      process.stdout.write(`  [a11y] ${name}: ${audit.violations.length} issues (${ids.join(", ")})\n`);
    }
  } catch {
    // Network errors, page-closed-during-analyze, etc — never fail
    // the smoke just because axe stumbled.
  }
}
async function shotDesktop(page: Page, name: string) {
  try {
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    await page.screenshot({ path: join(SHOT_DESKTOP, `${name}.png`), fullPage: true });
    await runAxe(page, name);
  } catch {}
}
async function shotMobile(page: Page, name: string) {
  try {
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    await page.screenshot({ path: join(SHOT_MOBILE, `${name}.png`), fullPage: true });
  } catch {}
}

// ---------- auth helpers ----------
async function loginViaApi(ctx: BrowserContext): Promise<void> {
  const baseUrl = new URL(BASE_URL);
  const loginResp = await fetch(`${BASE_URL}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: USERNAME, password: PASSWORD }),
  });
  if (!loginResp.ok) {
    throw new Error(`POST /api/v1/auth/login → ${loginResp.status} ${await loginResp.text()}`);
  }
  const setCookie = loginResp.headers.get("set-cookie") ?? "";
  const sessionCookieMatch = setCookie.match(/(__Host-[^=]+)=([^;]+)/);
  if (!sessionCookieMatch) {
    throw new Error(`no __Host- session cookie in Set-Cookie: ${setCookie}`);
  }
  await ctx.addCookies([
    {
      name: sessionCookieMatch[1],
      value: sessionCookieMatch[2],
      domain: baseUrl.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

async function elevateToAdmin(page: Page): Promise<void> {
  const resp = await page.request.post(`${BASE_URL}/api/v1/auth/elevate`, {
    headers: { "Content-Type": "application/json" },
    data: { target_mode: "admin", password: PASSWORD },
  });
  if (!resp.ok()) {
    throw new Error(`POST /api/v1/auth/elevate → ${resp.status()} ${await resp.text()}`);
  }
}

async function dropToUser(page: Page): Promise<void> {
  // The drop path is /auth/logout-elevation (not /auth/elevate with
  // target_mode=user — that's rejected by INVALID_TARGET_MODE).
  const resp = await page.request.post(`${BASE_URL}/api/v1/auth/logout-elevation`);
  if (!resp.ok()) {
    throw new Error(`POST /api/v1/auth/logout-elevation → ${resp.status()}`);
  }
}

// ---------- baseline real-resource counts (for end-of-run sanity) ----------
type Counts = {
  regions: number;
  serviceAccounts: number;
  webhooks: number;
  backups: number;
  federations: number;
};

async function countRealResources(page: Page): Promise<Counts> {
  async function getJsonLen(url: string): Promise<number> {
    const r = await page.request.get(`${BASE_URL}${url}`);
    if (!r.ok()) return -1;
    const body = await r.json().catch(() => null);
    if (Array.isArray(body)) return body.length;
    if (Array.isArray(body?.items)) return body.items.length;
    return -1;
  }
  return {
    regions: await getJsonLen("/api/v1/user/regions"),
    serviceAccounts: await getJsonLen("/api/v1/admin/service-accounts"),
    webhooks: await getJsonLen("/api/v1/user/webhooks"),
    backups: await getJsonLen("/api/v1/user/backups"),
    federations: await getJsonLen("/api/v1/user/federated-buckets"),
  };
}

// Count only NON-smoke entries — operator's real resources, the
// thing the safety check actually cares about. Smoke leftovers
// (whether from this run or a prior one) are excluded so they don't
// trigger the drift alarm.
async function countOperatorReal(page: Page): Promise<Counts> {
  async function countNonSmoke(url: string, nameField: string): Promise<number> {
    const r = await page.request.get(`${BASE_URL}${url}`);
    if (!r.ok()) return -1;
    const body = await r.json().catch(() => null);
    const arr = Array.isArray(body) ? body : Array.isArray(body?.items) ? body.items : null;
    if (!arr) return -1;
    return arr.filter(
      (it: any) => !((it[nameField] ?? "") as string).startsWith("smoke-") && !it.revokedAt,
    ).length;
  }
  return {
    regions: await countNonSmoke("/api/v1/user/regions", "alias"),
    serviceAccounts: await countNonSmoke("/api/v1/admin/service-accounts", "name"),
    webhooks: await countNonSmoke("/api/v1/user/webhooks", "name"),
    backups: await countNonSmoke("/api/v1/user/backups", "name"),
    federations: await countNonSmoke("/api/v1/user/federated-buckets", "name"),
  };
}

function countsEqual(a: Counts, b: Counts): boolean {
  return (
    a.regions === b.regions &&
    a.serviceAccounts === b.serviceAccounts &&
    a.webhooks === b.webhooks &&
    a.backups === b.backups &&
    a.federations === b.federations
  );
}

// ---------- ephemeral resource tracking ----------
type Ephemeral = { kind: string; id: string; name: string };
const ephemerals: Ephemeral[] = [];

function trackEphemeral(kind: string, id: string, name: string) {
  ephemerals.push({ kind, id, name });
  info(`  tracked ephemeral ${kind}: ${name} (${id})`);
}

// ---------- main flow (sections wired in subsequent commits) ----------
async function main(): Promise<number> {
  info("basement comprehensive UI smoke");
  info(`target:      ${BASE_URL}`);
  info(`user:        ${USERNAME}`);
  info(`screenshots: ${SHOT_ROOT}`);
  info(`ephemeral:   ${EPH_PREFIX}-*`);

  let browser: Browser | undefined;
  let desktopCtx: BrowserContext | undefined;
  let mobileCtx: BrowserContext | undefined;
  let desktop: Page | undefined;
  let mobile: Page | undefined;
  let baseline: Counts | undefined;

  try {
    browser = await chromium.launch({ headless: true });
    desktopCtx = await browser.newContext({
      viewport: { width: 1280, height: 900 },
      ignoreHTTPSErrors: false,
    });
    mobileCtx = await browser.newContext({
      viewport: { width: 375, height: 667 },
      deviceScaleFactor: 2,
      isMobile: true,
      hasTouch: true,
      ignoreHTTPSErrors: false,
    });

    desktop = await desktopCtx.newPage();
    mobile = await mobileCtx.newPage();
    attachListeners(desktop);
    attachListeners(mobile);

    // ============================================================
    // 0. Auth bootstrap (both contexts)
    // ============================================================
    section("[0] auth bootstrap");
    await check("login via API + inject session cookie into desktop context", async () => {
      await loginViaApi(desktopCtx!);
      await desktop!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      await desktop!.waitForURL(/\/files(\?|$)/, { timeout: 10_000 });
    });
    await check("login via API + inject session cookie into mobile context", async () => {
      await loginViaApi(mobileCtx!);
      await mobile!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      await mobile!.waitForURL(/\/files(\?|$)/, { timeout: 10_000 });
    });

    // Elevate to admin so /api/v1/admin/* counts work in the baseline
    // (and so admin-route checks below have permissions). Elevation
    // is idempotent — admin-mode pages re-fire it as needed too.
    await check("elevate desktop context to admin mode", async () => {
      await elevateToAdmin(desktop!);
      const me = await desktop!.request.get(`${BASE_URL}/api/v1/auth/me`);
      const body = await me.json();
      if (body.mode !== "admin") throw new Error(`expected mode=admin, got ${body.mode}`);
    });

    // Reap leftover smoke-* resources from prior runs that stalled
    // before cleanup. Best-effort — failures here are just logged.
    // We only reap entries whose name STARTS with `smoke-` since the
    // operator's real resources don't use that prefix. This keeps the
    // baseline meaningful (smoke leftovers don't inflate the "real"
    // count and trigger drift alarms at end-of-run).
    await check("opportunistically reap stale smoke-* leftovers from prior runs", async () => {
      const targets: { kind: string; listUrl: string; delUrl: (id: string) => string }[] = [
        { kind: "sa", listUrl: "/api/v1/admin/service-accounts", delUrl: (id) => `/api/v1/admin/service-accounts/${id}` },
        { kind: "webhook", listUrl: "/api/v1/user/webhooks", delUrl: (id) => `/api/v1/user/webhooks/${id}` },
        { kind: "backup", listUrl: "/api/v1/user/backups", delUrl: (id) => `/api/v1/user/backups/${id}` },
        { kind: "federation", listUrl: "/api/v1/user/federated-buckets", delUrl: (id) => `/api/v1/user/federated-buckets/${id}` },
      ];
      let reaped = 0;
      for (const t of targets) {
        const r = await desktop!.request.get(`${BASE_URL}${t.listUrl}`);
        if (!r.ok()) continue;
        const body = await r.json().catch(() => null);
        const arr = Array.isArray(body) ? body : Array.isArray(body?.items) ? body.items : [];
        const stale = arr.filter(
          (it: any) => (it.name ?? "").startsWith("smoke-") && !it.revokedAt,
        );
        for (const it of stale) {
          const d = await desktop!.request.delete(`${BASE_URL}${t.delUrl(it.id)}`);
          if (d.ok()) {
            reaped++;
          } else {
            warnLine(`  failed to reap ${t.kind} ${it.name} (${it.id}): ${d.status()}`);
          }
        }
      }
      if (reaped > 0) info(`  reaped ${reaped} stale smoke leftover(s)`);
      else info("  no stale smoke leftovers to reap");
    });

    // Baseline counts BEFORE any mutation. End-of-run compares
    // against this; mismatch = real data was touched. We count only
    // operator-real resources (non-smoke, non-revoked) so leftovers
    // from this or any prior run don't trigger drift alarms.
    await check("record baseline counts of operator-real resources", async () => {
      baseline = await countOperatorReal(desktop!);
      info(`  baseline: regions=${baseline.regions} sa=${baseline.serviceAccounts} webhooks=${baseline.webhooks} backups=${baseline.backups} federations=${baseline.federations}`);
    });

    // Discover a real region + bucket so route walks below have
    // concrete params. These are READ-ONLY — no mutations.
    let realRegionId = "";
    let realBucketId = "";
    let realClusterId = "";
    await check("discover a real region + bucket + cluster for route walks (READ-ONLY)", async () => {
      const rResp = await desktop!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (rResp.ok()) {
        const arr = await rResp.json();
        if (Array.isArray(arr) && arr.length > 0) realRegionId = arr[0].id;
      }
      if (realRegionId) {
        const bResp = await desktop!.request.get(`${BASE_URL}/api/v1/user/regions/${realRegionId}/buckets`);
        if (bResp.ok()) {
          const body = await bResp.json();
          const buckets = Array.isArray(body) ? body : body?.buckets ?? [];
          if (buckets.length > 0) realBucketId = buckets[0].id;
        }
      }
      const cResp = await desktop!.request.get(`${BASE_URL}/api/v1/admin/clusters`);
      if (cResp.ok()) {
        const arr = await cResp.json();
        if (Array.isArray(arr) && arr.length > 0) realClusterId = arr[0].id;
      }
      info(`  region=${realRegionId || "<none>"} bucket=${realBucketId || "<none>"} cluster=${realClusterId || "<none>"}`);
    });

    // ============================================================
    // A. Route enumeration — visit every URL pattern from frontend/src/routes/
    // ============================================================
    section("[A] route enumeration — every route walked desktop + console-error gate");

    // routes[].requiresAdmin gates whether we re-elevate first.
    // routes[].mobile=false skips the mobile re-run (e.g. wizards
    // that don't have a mobile layout yet).
    type RouteSpec = {
      path: string;
      key: string;
      requiresAdmin?: boolean;
      mobile?: boolean;
      requiresRegion?: boolean;
      requiresBucket?: boolean;
      requiresCluster?: boolean;
      // an optional minimal assertion that must hold on the rendered page
      assertText?: string;
    };

    const routes: RouteSpec[] = [
      // Public / unauthenticated surfaces
      { path: "/admin/login", key: "admin-login", assertText: "Sign in" },
      { path: "/share/notarealtokenforsmoke", key: "share-bogus-token", assertText: "not found" },

      // User shell
      { path: "/", key: "root", assertText: "My Regions" },
      { path: "/files", key: "files-home", assertText: "My Regions" },
      { path: "/files/keys", key: "files-keys", assertText: "My Keys" },
      { path: "/files/keys/new", key: "files-keys-new", assertText: "Add a key" },
      { path: "/files/regions/new", key: "files-regions-new-redirect", assertText: "Add a key" },
      { path: "/files/shares", key: "files-shares", assertText: "Shares" },
      { path: "/files/syncs", key: "files-syncs", assertText: "Cross-cluster" },
      { path: "/files/syncs/new", key: "files-syncs-new" },
      { path: "/files/backups", key: "files-backups", assertText: "Backups" },
      { path: "/files/backups/new", key: "files-backups-new", assertText: "New backup" },
      { path: "/files/federated-buckets", key: "files-federated", assertText: "Federations" },
      { path: "/files/federated-buckets/new", key: "files-federated-new" },
      { path: "/files/webhooks", key: "files-webhooks", assertText: "Webhook" },
      { path: "/files/webhooks/new", key: "files-webhooks-new" },

      // User shell w/ params (only if we have a real region / bucket)
      { path: "/files/{regionId}", key: "files-region", requiresRegion: true },
      { path: "/files/{regionId}/b/{bid}", key: "files-region-bucket", requiresRegion: true, requiresBucket: true },

      // Admin
      { path: "/admin", key: "admin-root", requiresAdmin: true },
      { path: "/admin/system", key: "admin-system", requiresAdmin: true },
      { path: "/admin/users", key: "admin-users", requiresAdmin: true, assertText: "User Management" },
      { path: "/admin/users/new", key: "admin-users-new", requiresAdmin: true },
      { path: "/admin/clusters", key: "admin-clusters", requiresAdmin: true, assertText: "Clusters" },
      { path: "/admin/clusters/new", key: "admin-clusters-new", requiresAdmin: true },
      { path: "/admin/buckets", key: "admin-buckets", requiresAdmin: true },
      // v1.11.0.15: /admin/keys removed — keys are per-cluster only.
      // Covered indirectly via the /admin/clusters/{cid} detail page
      // entry below (which renders this cluster's keys section).
      { path: "/admin/audit", key: "admin-audit", requiresAdmin: true, assertText: "Audit log" },
      { path: "/admin/policies", key: "admin-policies", requiresAdmin: true },
      { path: "/admin/service-accounts", key: "admin-sa", requiresAdmin: true },
      { path: "/admin/service-accounts/new", key: "admin-sa-new", requiresAdmin: true },
      { path: "/admin/migrate", key: "admin-migrate", requiresAdmin: true },

      // Admin w/ cluster param
      { path: "/admin/clusters/{cid}", key: "admin-cluster-detail", requiresAdmin: true, requiresCluster: true },
      { path: "/admin/clusters/{cid}/edit", key: "admin-cluster-edit", requiresAdmin: true, requiresCluster: true },
      { path: "/admin/clusters/{cid}/layout", key: "admin-cluster-layout", requiresAdmin: true, requiresCluster: true },
      { path: "/admin/clusters/{cid}/scrub", key: "admin-cluster-scrub", requiresAdmin: true, requiresCluster: true },
    ];

    function expandPath(p: string): string | null {
      let out = p;
      if (out.includes("{regionId}")) {
        if (!realRegionId) return null;
        out = out.replace("{regionId}", realRegionId);
      }
      if (out.includes("{bid}")) {
        if (!realBucketId) return null;
        out = out.replace("{bid}", realBucketId);
      }
      if (out.includes("{cid}")) {
        if (!realClusterId) return null;
        out = out.replace("{cid}", realClusterId);
      }
      return out;
    }

    let routeWalkErrors = 0;
    for (const spec of routes) {
      const url = expandPath(spec.path);
      if (url === null) {
        skipLine(`[A] visit ${spec.path}`, "required param not discoverable");
        continue;
      }
      // Re-elevate before EVERY admin nav — the elevation modal
      // middleware can pop a popup for any admin-mode capability
      // probe, and the smoke can't dismiss it cleanly. Cheap call
      // (one POST), worth the safety.
      //
      // We ALSO hit /auth/me after the elevation POST so the SPA's
      // useAuthMode React context refetches BEFORE the next goto.
      // Without this the AppShell's mode-coupling effect (v1.9.0e.2)
      // sees stale `mode=user` and silently redirects /admin/* to
      // /files in the brief hydration window between cookie-update
      // and React-state-update.
      if (spec.requiresAdmin) {
        await elevateToAdmin(desktop!).catch(() => {});
        // Prime the auth/me cache so the page's React Query sees
        // mode=admin synchronously on render.
        await desktop!.request.get(`${BASE_URL}/api/v1/auth/me`).catch(() => {});
      }
      await check(`[A] visit ${spec.path} (desktop)`, async () => {
        // Snapshot the error counts BEFORE navigation so we attribute
        // any new console/page errors to this specific route.
        const errBefore = consoleErrors.length + pageErrors.length;
        const resp = await desktop!.goto(`${BASE_URL}${url}`, { waitUntil: "networkidle", timeout: 20_000 });
        if (resp && resp.status() >= 500) {
          throw new Error(`HTTP ${resp.status()} from server on ${url}`);
        }
        // Wait for an h1 OR a "not found"-ish render to settle (the
        // share/bogus-token path has no h1; that's fine).
        await desktop!.waitForSelector('h1, body', { timeout: 10_000 }).catch(() => {});
        // Optional text gate. Wait up to 5s for the text to settle —
        // route components hydrate asynchronously, so a snap-shot
        // check immediately after networkidle can race the React
        // first paint.
        if (spec.assertText) {
          const has = await desktop!
            .waitForFunction(
              (t: string) => (document.body.innerText || "").toLowerCase().includes(t.toLowerCase()),
              spec.assertText,
              { timeout: 5_000 },
            )
            .then(() => true)
            .catch(() => false);
          if (!has) {
            const actualH1 = await desktop!
              .locator("h1")
              .first()
              .textContent()
              .catch(() => "<none>");
            const finalUrl = desktop!.url();
            reportBug(
              "route-walk",
              `${spec.path} did not surface expected text "${spec.assertText}" (final url=${finalUrl}, h1=${actualH1?.slice(0, 60) ?? "<none>"})`,
            );
          }
        }
        await shotDesktop(desktop!, `A-${spec.key}`);
        const errAfter = consoleErrors.length + pageErrors.length;
        if (errAfter > errBefore) {
          routeWalkErrors += errAfter - errBefore;
          reportBug("console-error", `${spec.path} produced ${errAfter - errBefore} new console/page error(s)`);
        }
      });
    }
    if (routeWalkErrors > 0) {
      warnLine(`route walk produced ${routeWalkErrors} cumulative console/page error(s)`);
    }

    // ============================================================
    // E. Mobile viewport re-run of read-only routes
    // ============================================================
    section("[E] mobile viewport (375x667) re-run of read-only routes");

    // Mobile context needs its own elevation so admin endpoints work.
    await check("[E] elevate mobile context to admin", async () => {
      await elevateToAdmin(mobile!);
    });

    for (const spec of routes) {
      if (spec.mobile === false) continue;
      const url = expandPath(spec.path);
      if (url === null) continue;
      if (spec.requiresAdmin) {
        await elevateToAdmin(mobile!).catch(() => {});
      }
      await check(`[E] visit ${spec.path} (mobile)`, async () => {
        const resp = await mobile!.goto(`${BASE_URL}${url}`, { waitUntil: "networkidle", timeout: 20_000 });
        if (resp && resp.status() >= 500) {
          throw new Error(`HTTP ${resp.status()} from server on ${url}`);
        }
        await mobile!.waitForSelector('h1, body', { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, `E-${spec.key}`);
      });
    }

    // Quick mobile-specific assertions: nav scroll + touch targets.
    await check("[E] mobile /files: primary nav scrollable horizontally", async () => {
      await mobile!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      const navOverflow = await mobile!.evaluate(() => {
        const nav = document.querySelector('nav[aria-label="Primary"]');
        if (!nav) return null;
        const cs = window.getComputedStyle(nav);
        return { overflow: cs.overflowX, scrollWidth: nav.scrollWidth, clientWidth: nav.clientWidth };
      });
      if (!navOverflow) {
        reportBug("mobile-nav", "Primary nav element not found on mobile /files");
        return;
      }
      // We expect either overflow-x auto/scroll, OR the content to fit
      // within the viewport (no horizontal overflow needed).
      const ok = ["auto", "scroll"].includes(navOverflow.overflow) || navOverflow.scrollWidth <= navOverflow.clientWidth;
      if (!ok) {
        reportBug("mobile-nav", `Primary nav overflows but isn't scrollable: ${JSON.stringify(navOverflow)}`);
      }
    });

    // ============================================================
    // B. State coverage — empty-state screenshots, table-row inspection
    // ============================================================
    section("[B] state coverage — empty-state + populated-state screenshots");

    await check("[B] /files/syncs empty-state screenshot", async () => {
      await desktop!.goto(`${BASE_URL}/files/syncs`, { waitUntil: "networkidle" });
      await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
      await shotDesktop(desktop!, "B-state-syncs");
    });

    await check("[B] /files/shares empty-state screenshot", async () => {
      await desktop!.goto(`${BASE_URL}/files/shares`, { waitUntil: "networkidle" });
      await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
      await shotDesktop(desktop!, "B-state-shares");
    });

    await check("[B] /files/backups empty-state screenshot", async () => {
      await desktop!.goto(`${BASE_URL}/files/backups`, { waitUntil: "networkidle" });
      await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
      await shotDesktop(desktop!, "B-state-backups");
    });

    await check("[B] /files/webhooks empty-state screenshot", async () => {
      await desktop!.goto(`${BASE_URL}/files/webhooks`, { waitUntil: "networkidle" });
      await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
      await shotDesktop(desktop!, "B-state-webhooks");
    });

    await check("[B] /files/federated-buckets empty-state screenshot", async () => {
      await desktop!.goto(`${BASE_URL}/files/federated-buckets`, { waitUntil: "networkidle" });
      await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
      await shotDesktop(desktop!, "B-state-federated");
    });

    // Object browser populated state — if we have a bucket, walk into
    // it and capture a screenshot at the listing root.
    if (realRegionId && realBucketId) {
      await check("[B] /files/{r}/b/{b} object browser populated state", async () => {
        await desktop!.goto(`${BASE_URL}/files/${realRegionId}/b/${realBucketId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "B-state-object-browser");
        // Mobile too.
        await mobile!.goto(`${BASE_URL}/files/${realRegionId}/b/${realBucketId}`, {
          waitUntil: "networkidle",
        });
        await mobile!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, "B-state-object-browser");
      });
    }

    // /admin/clusters populated state — walk through cluster row.
    if (realClusterId) {
      await check("[B] /admin/clusters/{cid} populated screenshot", async () => {
        await elevateToAdmin(desktop!);
        await desktop!.goto(`${BASE_URL}/admin/clusters/${realClusterId}`, { waitUntil: "networkidle" });
        await desktop!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "B-state-cluster-detail");
      });
    }

    // v1.11.0.15: /admin/keys populated state removed alongside the
    // route itself. Keys are per-cluster only; the cluster detail
    // page's keys section is the canonical view and is screenshotted
    // under the cluster-detail block above when realClusterId resolves.

    await check("[B] /admin/audit populated screenshot", async () => {
      await elevateToAdmin(desktop!);
      await desktop!.goto(`${BASE_URL}/admin/audit`, { waitUntil: "networkidle" });
      // Wait extra for the audit table — it can be slow to render.
      await desktop!
        .waitForFunction(() => /Audit/i.test(document.body.innerText || ""), null, { timeout: 8_000 })
        .catch(() => {});
      await shotDesktop(desktop!, "B-state-admin-audit");
    });

    // Mobile equivalents of the populated states.
    // v1.11.0.15: mobile /admin/keys removed alongside the route.
    // Cluster-detail mobile screenshot below covers the per-cluster
    // keys section.

    if (realClusterId) {
      await check("[B] mobile /admin/clusters/{cid} populated screenshot", async () => {
        await elevateToAdmin(mobile!);
        await mobile!.goto(`${BASE_URL}/admin/clusters/${realClusterId}`, { waitUntil: "networkidle" });
        await mobile!.waitForSelector("h1", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, "B-state-cluster-detail");
      });
    }

    // ============================================================
    // C+D. Form validation + dialog/modal walks (ephemeral CRUD)
    // ============================================================
    section("[C+D] form validation + ephemeral CRUD via API (most reliable)");

    // Strategy: form-walks via the UI are flaky against a live deploy
    // (toast timing, dialog z-index, focus management). The most
    // reliable way to exercise CREATE→VERIFY→DELETE for each resource
    // type is the API directly, with a parallel UI screenshot of the
    // form rendered. This keeps the destructive coverage deterministic
    // and surfaces UI bugs via screenshots without UI form races.

    // ---------- Service Account ----------
    await check("[C] ephemeral SA: create via API, screenshot detail, delete", async () => {
      const name = ephName("sa");
      const r = await desktop!.request.post(`${BASE_URL}/api/v1/admin/service-accounts`, {
        headers: { "Content-Type": "application/json" },
        data: { name, capabilities: [{ id: "bucket:view", scope: "bucket:*:*" }] },
      });
      if (!r.ok()) throw new Error(`POST /admin/service-accounts → ${r.status()} ${await r.text()}`);
      // Response shape: { serviceAccount: {id, ...}, secret: "..." }
      // The secret is shown once; we don't need to use it for the
      // smoke since cleanup goes through the admin DELETE endpoint.
      const body = await r.json();
      const saId = (body.serviceAccount?.id ?? body.id) as string;
      if (!saId) throw new Error(`SA create response missing id: ${JSON.stringify(body).slice(0, 200)}`);
      trackEphemeral("sa", saId, name);

      // Navigate the UI to /admin/service-accounts to confirm it lists.
      await desktop!.goto(`${BASE_URL}/admin/service-accounts`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-sa-list-with-ephemeral");

      // Detail page render — if the page exists.
      await desktop!.goto(`${BASE_URL}/admin/service-accounts/${saId}`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-sa-detail");
    });

    // ---------- Webhook ----------
    await check("[C] ephemeral webhook: create via API, screenshot list + detail, delete", async () => {
      const name = ephName("webhook");
      const r = await desktop!.request.post(`${BASE_URL}/api/v1/user/webhooks`, {
        headers: { "Content-Type": "application/json" },
        data: {
          name,
          targetUrl: "https://example.invalid/smoke-webhook",
          events: ["object.created"],
          enabled: true,
        },
      });
      if (!r.ok()) throw new Error(`POST /user/webhooks → ${r.status()} ${await r.text()}`);
      const created = await r.json();
      trackEphemeral("webhook", created.id as string, name);

      await desktop!.goto(`${BASE_URL}/files/webhooks`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-webhook-list-with-ephemeral");

      await desktop!.goto(`${BASE_URL}/files/webhooks/${created.id}`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-webhook-detail");
    });

    // ---------- Backup ----------
    await check("[C] ephemeral backup: create via API (manual+disabled+mirror), screenshot, delete", async () => {
      if (!realRegionId || !realBucketId) {
        warnLine("no region/bucket — skipping backup CRUD");
        return;
      }
      const name = ephName("backup");
      const r = await desktop!.request.post(`${BASE_URL}/api/v1/user/backups`, {
        headers: { "Content-Type": "application/json" },
        data: {
          name,
          srcRegionId: realRegionId,
          srcBucket: realBucketId,
          dstRegionId: realRegionId,
          dstBucket: realBucketId,
          schedule: "manual",
          disabled: true, // critical: NEVER runs against operator's real bucket
          mode: "mirror",
        },
      });
      if (!r.ok()) throw new Error(`POST /user/backups → ${r.status()} ${await r.text()}`);
      const created = await r.json();
      const backupId = created.id as string;
      trackEphemeral("backup", backupId, name);

      await desktop!.goto(`${BASE_URL}/files/backups`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-backup-list-with-ephemeral");

      await desktop!.goto(`${BASE_URL}/files/backups/${backupId}`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-backup-detail");

      // Restore wizard route renders the mirror-mode notice (not a wizard).
      await desktop!.goto(`${BASE_URL}/files/backups/${backupId}/restore`, { waitUntil: "networkidle" });
      await shotDesktop(desktop!, "C-backup-restore");
    });

    // ---------- Form validation walks via the UI ----------
    // For each new-resource form: load → submit blank → assert validation
    // fires → screenshot. We do NOT then submit valid data — the create
    // path is exercised via API above. This keeps the destructive
    // coverage scope tight and validates the validation gate itself.
    const validationForms = [
      {
        path: "/files/keys/new",
        key: "keys-new",
        submitText: /Add key|Save key|Add a key/i,
        validationCue: /required|invalid|cannot be|please/i,
      },
      {
        path: "/files/webhooks/new",
        key: "webhooks-new",
        submitText: /Create webhook|Save|Submit/i,
        validationCue: /required|invalid|cannot be|please|at least/i,
      },
      {
        path: "/files/federated-buckets/new",
        key: "fed-new",
        submitText: /Create|Continue|Next/i,
        validationCue: /required|invalid|cannot be|please|at least/i,
      },
      {
        path: "/admin/clusters/new",
        key: "clusters-new",
        submitText: /Add cluster|Connect|Create|Save/i,
        validationCue: /required|invalid|cannot be|please/i,
      },
      {
        path: "/admin/users/new",
        key: "users-new",
        submitText: /Create|Invite|Save/i,
        validationCue: /required|invalid|cannot be|please/i,
      },
    ];

    for (const f of validationForms) {
      await check(`[C] ${f.path}: blank submit fires validation`, async () => {
        if (f.path.startsWith("/admin")) await elevateToAdmin(desktop!).catch(() => {});
        await desktop!.goto(`${BASE_URL}${f.path}`, { waitUntil: "networkidle" });
        await desktop!.waitForSelector("form, h1", { timeout: 10_000 }).catch(() => {});
        // Capture pristine form state first.
        await shotDesktop(desktop!, `C-validation-${f.key}-pristine`);

        // Find a plausible submit button. If we can't find one we
        // warn-and-pass — many forms have a wizard "Next" button as
        // their primary action.
        const btn = desktop!
          .locator("button[type=submit], button")
          .filter({ hasText: f.submitText })
          .first();
        const btnCount = await btn.count();
        if (btnCount === 0) {
          warnLine(`  ${f.path}: no submit button matching ${f.submitText}, skipping blank-submit`);
          return;
        }
        await btn.click({ timeout: 5_000 }).catch(() => {});

        // Wait briefly for validation messages to render. We accept
        // EITHER an HTML5 :invalid input, a [role=alert] node, or
        // body text matching one of the cue patterns.
        const hadValidation = await desktop!
          .waitForFunction(
            (cue: string) => {
              const cueRe = new RegExp(cue, "i");
              const hasInvalidInput = document.querySelectorAll("input:invalid, textarea:invalid").length > 0;
              const hasAlert = document.querySelectorAll('[role="alert"], [aria-invalid="true"]').length > 0;
              const hasCue = cueRe.test(document.body.innerText || "");
              return hasInvalidInput || hasAlert || hasCue;
            },
            f.validationCue.source,
            { timeout: 4_000 },
          )
          .then(() => true)
          .catch(() => false);
        if (!hadValidation) {
          reportBug("form-validation", `${f.path}: blank submit did NOT fire any validation gate`);
        }
        await shotDesktop(desktop!, `C-validation-${f.key}`);
      });
    }

    // ---------- Modal walks ----------
    // v1.11.0.15: /admin/keys "+ New" dialog probe removed alongside
    // the route. Per-cluster key minting + delete is exercised end-to-end
    // by scripts/feature-smoke.ts against ephemeral garage-v2-test-*
    // clusters, which is the right place for destructive flows.

    // Elevation modal — drop to user, then navigate to /admin/* and verify
    // the elevation modal renders (we don't actually submit; just confirm
    // the modal appears and screenshot it).
    await check("[D] elevation modal renders when dropped to USER mode hitting /admin", async () => {
      await dropToUser(desktop!);
      await desktop!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });
      // The middleware either pops a password modal OR redirects to /files.
      // Accept either, but screenshot whichever rendered.
      const elevModal = desktop!.locator('input[type="password"], [role="dialog"]').first();
      const visible = await elevModal.isVisible({ timeout: 5_000 }).catch(() => false);
      const url = desktop!.url();
      if (!visible && !/files/.test(url)) {
        reportBug("elevation", `/admin/clusters in USER mode neither popped elevation modal nor redirected to /files (url=${url})`);
      }
      await shotDesktop(desktop!, "D-elevation-modal-or-redirect");
      // Re-elevate so subsequent checks continue working.
      await elevateToAdmin(desktop!);
    });

    // ============================================================
    // F. Auth-state coverage — drop-to-user nav, elevate-back nav
    // ============================================================
    section("[F] auth-state navigation (USER ↔ ADMIN per ADR-0003 + v1.9.0e.2)");

    await check("[F] drop-to-user from /admin lands on /files (v1.9.0e.2)", async () => {
      await elevateToAdmin(desktop!);
      await desktop!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });
      await dropToUser(desktop!);
      // Reload — the AuthModeProvider needs the new cookie to re-evaluate.
      // Per v1.9.0e.2 tight coupling: drop = /files.
      await desktop!.goto(`${BASE_URL}/`, { waitUntil: "networkidle" });
      await desktop!.waitForURL(/\/files(\?|$)/, { timeout: 10_000 });
      await shotDesktop(desktop!, "F-after-drop-to-user");
    });

    await check("[F] elevate-from-user → /api/v1/auth/me reports admin", async () => {
      // We can't reliably replicate the in-page elevation click from
      // outside the React tree — the UserMenu's elevation flow uses
      // the useAuthMode provider which holds its own state. The
      // sub-check we CAN make is that /auth/me reports admin after
      // a server-side elevate POST. The UI nav side is exercised by
      // the route walks in [A] above.
      await desktop!.goto("about:blank");
      await elevateToAdmin(desktop!);
      const meResp = await desktop!.request.get(`${BASE_URL}/api/v1/auth/me`);
      const me = await meResp.json();
      if (me.mode !== "admin") throw new Error(`expected mode=admin after elevate, got ${me.mode}`);

      // Bonus: navigate to /admin/clusters and assert URL ends up there
      // OR documents the AppShell timing flake as a non-fatal bug.
      await desktop!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });
      await desktop!.waitForURL(/\/admin\/clusters/, { timeout: 5_000 }).catch(() => {});
      const url = desktop!.url();
      if (!/\/admin\/clusters/.test(url)) {
        reportBug(
          "appshell-mode-coupling",
          `After elevate /admin/clusters bounced to ${url} — AppShell mode-coupling race (v1.9.0e.2 effect fires before useAuthMode rehydrates)`,
        );
      }
      await shotDesktop(desktop!, "F-after-elevate-back");
    });

    // ============================================================
    // G. WebDAV functional probe (gateway architecture, v1.9.0)
    // ============================================================
    section("[G] WebDAV gateway probe (v1.9.0)");

    await check("[G] OPTIONS /webdav/ returns DAV headers", async () => {
      const r = await desktop!.request.fetch(`${BASE_URL}/webdav/`, { method: "OPTIONS" });
      // Allow either 200 or 401 (some configs require basic auth on
      // OPTIONS). We only care that the response speaks DAV.
      if (r.status() >= 500) throw new Error(`OPTIONS /webdav/ → ${r.status()}`);
      const dav = r.headers()["dav"];
      if (!dav) {
        reportBug("webdav", `OPTIONS /webdav/ returned no DAV header (status=${r.status()})`);
        return;
      }
      // Standard DAV class indicators are 1, 2, 3.
      if (!/[123]/.test(dav)) {
        reportBug("webdav", `OPTIONS /webdav/ DAV header malformed: ${dav}`);
      }
    });

    await check("[G] PROPFIND /webdav/ — best-effort", async () => {
      // Caddy edge may strip nonstandard verbs; this is informational.
      const r = await desktop!.request.fetch(`${BASE_URL}/webdav/`, {
        method: "PROPFIND",
        headers: { Depth: "0" },
      });
      // 207 multi-status = healthy. 401 = needs basic auth (also ok —
      // the verb made it through the edge). 405/501 = the edge is
      // blocking PROPFIND, which we document but don't fail.
      const s = r.status();
      if (s === 207 || s === 401) {
        info(`  PROPFIND ok (status=${s})`);
      } else if (s === 405 || s === 501) {
        warnLine(`  PROPFIND blocked by edge (status=${s}) — non-fatal`);
      } else if (s >= 500) {
        reportBug("webdav", `PROPFIND /webdav/ → ${s} (server error)`);
      }
    });

    // ============================================================
    // H. PWA probe (manifest + service worker)
    // ============================================================
    section("[H] PWA assets");

    await check("[H] GET /manifest.webmanifest returns valid manifest", async () => {
      const r = await desktop!.request.get(`${BASE_URL}/manifest.webmanifest`);
      if (!r.ok()) throw new Error(`GET /manifest.webmanifest → ${r.status()}`);
      const body = await r.json().catch(() => null);
      if (!body || typeof body !== "object") throw new Error("manifest did not parse as JSON");
      for (const required of ["name", "short_name", "start_url", "display", "icons"]) {
        if (!(required in body)) {
          reportBug("pwa", `manifest missing required field: ${required}`);
        }
      }
    });

    await check("[H] GET /sw.js — present (status 200) or warn", async () => {
      const r = await desktop!.request.get(`${BASE_URL}/sw.js`);
      if (r.status() === 200) {
        // Sanity: ensure it's at least script-shaped.
        const body = await r.text();
        if (body.length < 50) {
          reportBug("pwa", `/sw.js is suspiciously short: ${body.length} bytes`);
        }
      } else if (r.status() === 404) {
        // Some PWA configs serve the SW from a different path.
        warnLine("  /sw.js returned 404 — PWA may be disabled or SW at different path");
      } else {
        reportBug("pwa", `/sw.js returned unexpected status ${r.status()}`);
      }
    });

    // ============================================================
    // I. Extended detail-page coverage (more screenshots, more states)
    // ============================================================
    section("[I] extended detail-page screenshots");

    // /admin/clusters/{cid} subpaths in detail for both viewports.
    if (realClusterId) {
      const subpaths = ["", "/edit", "/layout", "/scrub"];
      for (const sp of subpaths) {
        await check(`[I] /admin/clusters/{cid}${sp} (desktop+mobile dedicated)`, async () => {
          await elevateToAdmin(desktop!).catch(() => {});
          await elevateToAdmin(mobile!).catch(() => {});
          const url = `${BASE_URL}/admin/clusters/${realClusterId}${sp}`;
          await desktop!.goto(url, { waitUntil: "networkidle" });
          await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
          await shotDesktop(desktop!, `I-cluster-detail${sp.replace(/\//g, "-")}`);
          await mobile!.goto(url, { waitUntil: "networkidle" });
          await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
          await shotMobile(mobile!, `I-cluster-detail${sp.replace(/\//g, "-")}`);
        });
      }
    }

    // Bucket detail page (admin side) + lifecycle subpage if it exists.
    if (realClusterId) {
      await check("[I] /admin/clusters/{cid}/buckets/{bid} discovery + screenshot", async () => {
        await elevateToAdmin(desktop!).catch(() => {});
        const r = await desktop!.request.get(`${BASE_URL}/api/v1/admin/clusters/${realClusterId}/buckets`);
        if (!r.ok()) {
          warnLine(`  admin buckets list → ${r.status()}; skipping bucket detail`);
          return;
        }
        const buckets = await r.json();
        if (!Array.isArray(buckets) || buckets.length === 0) {
          warnLine("  no buckets in admin cluster; skipping bucket detail");
          return;
        }
        const bid = buckets[0].id;
        await desktop!.goto(`${BASE_URL}/admin/clusters/${realClusterId}/buckets/${bid}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "I-admin-bucket-detail");
        await mobile!.goto(`${BASE_URL}/admin/clusters/${realClusterId}/buckets/${bid}`, {
          waitUntil: "networkidle",
        });
        await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, "I-admin-bucket-detail");

        // Lifecycle new sub-route (v1.x lifecycle UI).
        await desktop!.goto(
          `${BASE_URL}/admin/clusters/${realClusterId}/buckets/${bid}/lifecycle/new`,
          { waitUntil: "networkidle" },
        );
        await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "I-admin-bucket-lifecycle-new");
      });

      await check("[I] /admin/clusters/{cid}/keys/{kid} discovery + screenshot", async () => {
        await elevateToAdmin(desktop!).catch(() => {});
        const r = await desktop!.request.get(`${BASE_URL}/api/v1/admin/clusters/${realClusterId}/keys`);
        if (!r.ok()) {
          warnLine(`  admin keys list → ${r.status()}; skipping key detail`);
          return;
        }
        const keys = await r.json();
        if (!Array.isArray(keys) || keys.length === 0) {
          warnLine("  no keys in admin cluster; skipping key detail");
          return;
        }
        const kid = keys[0].id;
        await desktop!.goto(`${BASE_URL}/admin/clusters/${realClusterId}/keys/${kid}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "I-admin-key-detail");
        await mobile!.goto(`${BASE_URL}/admin/clusters/${realClusterId}/keys/${kid}`, {
          waitUntil: "networkidle",
        });
        await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, "I-admin-key-detail");
      });
    }

    // Object browser at different prefix depths.
    if (realRegionId && realBucketId) {
      const prefixes = ["", "?prefix=test%2F", "?prefix=nope%2F"];
      for (const qs of prefixes) {
        await check(`[I] object browser ${qs || "<root>"}`, async () => {
          const u = `${BASE_URL}/files/${realRegionId}/b/${realBucketId}${qs}`;
          await desktop!.goto(u, { waitUntil: "networkidle" });
          await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
          const safeName = qs.replace(/[^a-z0-9]/gi, "_") || "root";
          await shotDesktop(desktop!, `I-object-browser-${safeName}`);
          await mobile!.goto(u, { waitUntil: "networkidle" });
          await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
          await shotMobile(mobile!, `I-object-browser-${safeName}`);
        });
      }
    }

    // Region-detail page (under /files/{regionId}/) — the user-side
    // bucket list for one region.
    if (realRegionId) {
      await check("[I] /files/{regionId} bucket list (desktop + mobile)", async () => {
        await desktop!.goto(`${BASE_URL}/files/${realRegionId}`, { waitUntil: "networkidle" });
        await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, "I-files-region");
        await mobile!.goto(`${BASE_URL}/files/${realRegionId}`, { waitUntil: "networkidle" });
        await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, "I-files-region");
      });
    }

    // User shell pages — full screenshot of each at both viewports
    // explicitly, even though [A]/[E] already captured them. This
    // gives the operator a denser "what each page looked like THIS
    // deploy" reference grid.
    const denseUserPages = [
      { p: "/files", k: "files" },
      { p: "/files/keys", k: "files-keys" },
      { p: "/files/keys/new", k: "files-keys-new" },
      { p: "/files/shares", k: "files-shares" },
      { p: "/files/syncs", k: "files-syncs" },
      { p: "/files/backups", k: "files-backups" },
      { p: "/files/backups/new", k: "files-backups-new" },
      { p: "/files/federated-buckets", k: "files-federated" },
      { p: "/files/federated-buckets/new", k: "files-federated-new" },
      { p: "/files/webhooks", k: "files-webhooks" },
      { p: "/files/webhooks/new", k: "files-webhooks-new" },
    ];
    for (const u of denseUserPages) {
      await check(`[I] dense capture ${u.p}`, async () => {
        await desktop!.goto(`${BASE_URL}${u.p}`, { waitUntil: "networkidle" });
        await desktop!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotDesktop(desktop!, `I-dense-${u.k}`);
        await mobile!.goto(`${BASE_URL}${u.p}`, { waitUntil: "networkidle" });
        await mobile!.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
        await shotMobile(mobile!, `I-dense-${u.k}`);
      });
    }

    // Touch-target heuristic (also belongs to [E] mobile section).
    section("[E] mobile touch-target audit");
    await check("[E] mobile /files: touch targets >= 44px tall (heuristic)", async () => {
      await mobile!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      const small = await mobile!.evaluate(() => {
        const interactive = Array.from(document.querySelectorAll('button, a[href], [role="button"]'));
        const bad: { tag: string; text: string; h: number }[] = [];
        for (const el of interactive) {
          const r = el.getBoundingClientRect();
          // Skip zero-sized (off-screen / hidden) elements
          if (r.width === 0 && r.height === 0) continue;
          // Skip elements inside the version-pill area (intentionally small)
          if (el.closest('[data-testid="version-pill"]')) continue;
          if (r.height < 44) {
            const text = (el.textContent ?? "").trim().slice(0, 30);
            bad.push({ tag: el.tagName.toLowerCase(), text, h: Math.round(r.height) });
          }
        }
        return bad.slice(0, 5);
      });
      if (small.length > 0) {
        reportBug("mobile-touch-targets", `Found ${small.length}+ interactive elements <44px tall on /files: ${JSON.stringify(small)}`);
      }
    });

  } finally {
    // ============================================================
    // CLEANUP — runs even if checks above threw. Two passes:
    //   1. Tracked-ephemeral reap (resources THIS run created)
    //   2. Broad smoke-* sweep (resources from any run, including
    //      ones whose tracking we lost mid-flight)
    // ============================================================
    section("[cleanup] reaping ephemeral resources");
    if (desktop && ephemerals.length > 0) {
      for (const e of ephemerals.slice().reverse()) {
        try {
          let url = "";
          switch (e.kind) {
            case "region": url = `/api/v1/user/regions/${e.id}`; break;
            case "sa": url = `/api/v1/admin/service-accounts/${e.id}`; break;
            case "webhook": url = `/api/v1/user/webhooks/${e.id}`; break;
            case "backup": url = `/api/v1/user/backups/${e.id}`; break;
            case "federation": url = `/api/v1/user/federated-buckets/${e.id}`; break;
            default:
              warnLine(`unknown ephemeral kind '${e.kind}' — skipping cleanup for ${e.id}`);
              continue;
          }
          const r = await desktop.request.delete(`${BASE_URL}${url}`);
          if (!r.ok()) {
            failLine(`cleanup ${e.kind} ${e.name}`, `DELETE ${url} → ${r.status()}`);
            reportBug("cleanup", `Failed to reap ephemeral ${e.kind} ${e.name} (${e.id}) → HTTP ${r.status()}`);
          } else {
            info(`  reaped ${e.kind} ${e.name}`);
          }
        } catch (err) {
          failLine(`cleanup ${e.kind} ${e.name}`, err instanceof Error ? err.message : String(err));
        }
      }
    } else {
      info("  no tracked ephemerals to reap");
    }

    // Broad sweep — catches anything from this or prior runs that we
    // lost track of. Safe because every smoke-created resource uses
    // the `smoke-` prefix and the operator's real resources never do.
    if (desktop) {
      info("  broad sweep of smoke-* leftovers across all endpoints");
      const sweep: { kind: string; listUrl: string; nameField: string; delUrl: (id: string) => string }[] = [
        { kind: "sa", listUrl: "/api/v1/admin/service-accounts", nameField: "name", delUrl: (id) => `/api/v1/admin/service-accounts/${id}` },
        { kind: "webhook", listUrl: "/api/v1/user/webhooks", nameField: "name", delUrl: (id) => `/api/v1/user/webhooks/${id}` },
        { kind: "backup", listUrl: "/api/v1/user/backups", nameField: "name", delUrl: (id) => `/api/v1/user/backups/${id}` },
        { kind: "federation", listUrl: "/api/v1/user/federated-buckets", nameField: "name", delUrl: (id) => `/api/v1/user/federated-buckets/${id}` },
      ];
      for (const t of sweep) {
        const r = await desktop.request.get(`${BASE_URL}${t.listUrl}`);
        if (!r.ok()) continue;
        const body = await r.json().catch(() => null);
        const arr = Array.isArray(body) ? body : Array.isArray(body?.items) ? body.items : [];
        const targets = arr.filter(
          (it: any) => ((it[t.nameField] ?? "") as string).startsWith("smoke-") && !it.revokedAt,
        );
        for (const it of targets) {
          const d = await desktop.request.delete(`${BASE_URL}${t.delUrl(it.id)}`);
          if (!d.ok()) {
            warnLine(`  sweep failed for ${t.kind} ${it[t.nameField]} (${it.id}): ${d.status()}`);
          } else {
            info(`  swept ${t.kind} ${it[t.nameField]}`);
          }
        }
      }
    }

    // Real-resource count sanity check. Counts operator-real
    // resources only (excludes smoke-* leftovers).
    if (desktop && baseline) {
      const after = await countOperatorReal(desktop);
      info(`  after:    regions=${after.regions} sa=${after.serviceAccounts} webhooks=${after.webhooks} backups=${after.backups} federations=${after.federations}`);
      if (!countsEqual(baseline, after)) {
        failLine("operator-real count sanity", `baseline ${JSON.stringify(baseline)} != after ${JSON.stringify(after)}`);
        reportBug("safety", `Operator-real count drift detected — real data may have been mutated`);
      } else {
        passLine("operator-real count sanity (baseline == after)", 0);
      }
    }

    // Close browser regardless.
    await desktop?.close().catch(() => {});
    await mobile?.close().catch(() => {});
    await desktopCtx?.close().catch(() => {});
    await mobileCtx?.close().catch(() => {});
    await browser?.close().catch(() => {});
  }

  // ============================================================
  // Final summary
  // ============================================================
  const passed = results.filter((r) => r.ok && !r.skipped).length;
  const skipped = results.filter((r) => r.skipped).length;
  const failed = results.filter((r) => !r.ok).length;

  section("=== SUMMARY ===");
  process.stdout.write(`${C.green}passed:${C.reset}  ${passed}\n`);
  process.stdout.write(`${C.yellow}skipped:${C.reset} ${skipped}\n`);
  process.stdout.write(`${C.red}failed:${C.reset}  ${failed}\n`);
  process.stdout.write(`${C.dim}total:   ${results.length}${C.reset}\n`);
  process.stdout.write(`${C.dim}console errors: ${consoleErrors.length}, page errors: ${pageErrors.length}${C.reset}\n`);
  process.stdout.write(`${C.dim}screenshots: ${SHOT_ROOT}${C.reset}\n`);

  if (failed > 0) {
    section("=== FAILURES ===");
    for (const r of results.filter((x) => !x.ok)) {
      process.stderr.write(`${C.red}-${C.reset} ${r.name}: ${r.detail ?? ""}\n`);
    }
  }

  if (pageErrors.length > 0) {
    section("=== PAGE ERRORS ===");
    for (const e of pageErrors) {
      process.stderr.write(`${C.red}-${C.reset} ${e.url}: ${e.message}\n`);
    }
  }

  if (bugReport.length > 0) {
    section("=== BUG REPORT ===");
    for (const b of bugReport) {
      process.stderr.write(`${C.yellow}*${C.reset} ${b}\n`);
    }
  }

  // axe-core a11y rollup. Not a gate — surfaced so per-route trends
  // are visible in CI/local-run output. Cycle v1.11.0.12.
  if (AxeBuilder) {
    section("=== A11Y (axe-core) ===");
    if (axeAudits.length === 0) {
      process.stdout.write(`${C.green}no axe violations across screenshotted routes${C.reset}\n`);
    } else {
      const totalIssues = axeAudits.reduce((n, a) => n + a.violations, 0);
      process.stdout.write(`${C.yellow}${axeAudits.length} routes with axe violations (${totalIssues} issues total)${C.reset}\n`);
      for (const a of axeAudits) {
        process.stdout.write(`  - ${a.route} (${a.violations}): ${a.ids.join(", ")}\n`);
      }
    }
  } else {
    process.stdout.write(`${C.dim}[a11y] @axe-core/playwright not installed — a11y audit skipped (install with: pnpm -C frontend add -D @axe-core/playwright)${C.reset}\n`);
  }

  return failed === 0 ? 0 : 1;
}

const code = await main();
process.exit(code);
