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

// Filter the SA list down to user-real entries — the field that the
// baseline cares about. Smoke leftovers (name starting with "smoke-")
// from prior runs are counted separately so they don't trigger the
// drift alarm. Cleanup will reap them at end-of-run.
async function countRealSAs(page: Page): Promise<number> {
  const r = await page.request.get(`${BASE_URL}/api/v1/admin/service-accounts`);
  if (!r.ok()) return -1;
  const body = await r.json().catch(() => null);
  const arr = Array.isArray(body) ? body : Array.isArray(body?.items) ? body.items : null;
  if (!arr) return -1;
  return arr.filter((sa: any) => !(sa.name ?? "").startsWith("smoke-")).length;
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

    // Reap leftover smoke-* SAs from prior runs that stalled before
    // cleanup. Best-effort — failures here are just logged. We DON'T
    // reap leftover webhooks/backups/federations the same way because
    // (a) those endpoints don't currently leak, and (b) the operator
    // may have legitimate test resources named that way; SAs are
    // safe because the `smoke-` prefix is exclusive to this script.
    await check("opportunistically reap stale smoke-* SAs from prior runs", async () => {
      const r = await desktop!.request.get(`${BASE_URL}/api/v1/admin/service-accounts`);
      if (!r.ok()) {
        warnLine(`could not list SAs to reap: ${r.status()}`);
        return;
      }
      const body = await r.json().catch(() => null);
      const arr = Array.isArray(body) ? body : Array.isArray(body?.items) ? body.items : [];
      const stale = arr.filter((sa: any) => (sa.name ?? "").startsWith("smoke-") && !sa.revokedAt);
      if (stale.length === 0) {
        info("  no stale smoke SAs to reap");
        return;
      }
      info(`  reaping ${stale.length} stale smoke SA(s) from prior runs`);
      for (const sa of stale) {
        const d = await desktop!.request.delete(`${BASE_URL}/api/v1/admin/service-accounts/${sa.id}`);
        if (!d.ok()) {
          warnLine(`  failed to reap ${sa.name} (${sa.id}): ${d.status()}`);
        }
      }
    });

    // Baseline counts BEFORE any mutation. End-of-run compares
    // against this; mismatch = real data was touched. We use the
    // filtered SA count to ignore smoke-* leftovers from prior runs,
    // since cleanup at end-of-run will reap them.
    await check("record baseline counts of real resources", async () => {
      baseline = await countRealResources(desktop!);
      baseline.serviceAccounts = await countRealSAs(desktop!);
      info(`  baseline: regions=${baseline.regions} sa=${baseline.serviceAccounts} webhooks=${baseline.webhooks} backups=${baseline.backups} federations=${baseline.federations}`);
    });

    // Subsequent sections wired in follow-up commits.
    info("[scaffolding only — section bodies wired in subsequent commits]");

  } finally {
    // ============================================================
    // CLEANUP — runs even if checks above threw
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
      info("  no ephemeral resources to reap");
    }

    // Real-resource count sanity check.
    if (desktop && baseline) {
      const after = await countRealResources(desktop);
      after.serviceAccounts = await countRealSAs(desktop);
      info(`  after:    regions=${after.regions} sa=${after.serviceAccounts} webhooks=${after.webhooks} backups=${after.backups} federations=${after.federations}`);
      if (!countsEqual(baseline, after)) {
        failLine("real-resource count sanity", `baseline ${JSON.stringify(baseline)} != after ${JSON.stringify(after)}`);
        reportBug("safety", `Real-resource count drift detected — operator data may have been mutated`);
      } else {
        passLine("real-resource count sanity (baseline == after)", 0);
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
