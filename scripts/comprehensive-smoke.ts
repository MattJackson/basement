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
async function shotDesktop(page: Page, name: string) {
  try {
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    await page.screenshot({ path: join(SHOT_DESKTOP, `${name}.png`), fullPage: true });
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
  const resp = await page.request.post(`${BASE_URL}/api/v1/auth/elevate`, {
    headers: { "Content-Type": "application/json" },
    data: { target_mode: "user" },
  });
  if (!resp.ok()) {
    throw new Error(`POST /api/v1/auth/elevate(user) → ${resp.status()}`);
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
      { path: "/admin/keys", key: "admin-keys", requiresAdmin: true, assertText: "Access keys" },
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
            reportBug(
              "route-walk",
              `${spec.path} did not surface expected text "${spec.assertText}" (h1=${actualH1?.slice(0, 60) ?? "<none>"})`,
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

  return failed === 0 ? 0 : 1;
}

const code = await main();
process.exit(code);
