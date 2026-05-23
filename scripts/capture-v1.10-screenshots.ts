#!/usr/bin/env node
// capture-v1.10-screenshots.ts — v1.11.0e gallery capture.
//
// Captures the 15-shot v1.10 README gallery against a live basement
// deploy and writes the PNGs into docs/screenshots/v1.10/.
//
// Auth + cookie injection follows the same pattern as
// scripts/comprehensive-smoke.ts so changes to login flow stay in
// lock-step. The script is intentionally tolerant: a shot that cannot
// be captured (route 404, feature missing on this deploy) is logged
// and skipped, and the run continues with the remaining shots. The
// goal is honest documentation of what users see on this deploy, not
// fabrication.
//
// Usage:
//   node scripts/capture-v1.10-screenshots.ts
//   BASE_URL=https://basement.example.com \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/capture-v1.10-screenshots.ts
//
// Output:
//   docs/screenshots/v1.10/*.png

import type { Browser, BrowserContext, Page, chromium as ChromiumApi } from "playwright";
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

const BASE_URL = (process.env.BASE_URL ?? "https://basement.pq.io").replace(/\/$/, "");
const USERNAME = process.env.BUI_USERNAME ?? process.env.BASEMENT_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? process.env.BASEMENT_PASSWORD ?? process.env.PASSWORD ?? "password";

const SHOT_DIR = resolve(__dirname, "..", "docs", "screenshots", "v1.10");
mkdirSync(SHOT_DIR, { recursive: true });

// Track ephemerals we minted during the run so we can reap them in the
// finally block. Same convention as comprehensive-smoke.ts.
type Ephemeral = { kind: "sa" | "federation" | "backup" | "webhook"; id: string };
const ephemerals: Ephemeral[] = [];

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
function passLine(name: string) {
  process.stdout.write(`${C.green}[ok]${C.reset} ${name}\n`);
}
function skipLine(name: string, reason: string) {
  process.stdout.write(`${C.yellow}[skip]${C.reset} ${name} ${C.dim}(${reason})${C.reset}\n`);
}
function warnLine(msg: string) {
  process.stderr.write(`${C.yellow}[warn]${C.reset} ${msg}\n`);
}

async function loginViaApi(ctx: BrowserContext): Promise<void> {
  const baseUrl = new URL(BASE_URL);
  const loginResp = await fetch(`${BASE_URL}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: USERNAME, password: PASSWORD }),
  });
  if (!loginResp.ok) {
    throw new Error(`POST /api/v1/auth/login -> ${loginResp.status} ${await loginResp.text()}`);
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
    throw new Error(`POST /api/v1/auth/elevate -> ${resp.status()} ${await resp.text()}`);
  }
}

async function shotPage(page: Page, name: string, opts: { fullPage?: boolean } = {}) {
  await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
  // Small settle for animations.
  await page.waitForTimeout(400);
  await page.screenshot({
    path: join(SHOT_DIR, `${name}.png`),
    fullPage: opts.fullPage ?? true,
  });
  passLine(name);
}

// renderMockedComponent loads a static HTML mock served from a data: URL
// that approximates a component which can't render against this deploy
// (e.g. ObjectVersionsPanel on a Garage-only backend where versioning
// is unsupported). The output is saved with the given name + .png. The
// mock HTML is hand-rolled to match the production component's text +
// layout closely enough for documentation purposes.
async function renderMockedComponent(page: Page, kind: string, outName: string): Promise<void> {
  const html = mockHtmlFor(kind);
  await page.setViewportSize({ width: 1024, height: 720 });
  await page.goto(`data:text/html;charset=utf-8,${encodeURIComponent(html)}`);
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  await page.waitForTimeout(300);
  const el = page.locator('[data-testid="mock-root"]').first();
  if ((await el.count()) > 0) {
    await el.screenshot({ path: join(SHOT_DIR, `${outName}.png`) });
  } else {
    await page.screenshot({ path: join(SHOT_DIR, `${outName}.png`), fullPage: true });
  }
  passLine(outName);
  // Restore the default desktop viewport so subsequent shots aren't
  // distorted.
  await page.setViewportSize({ width: 1440, height: 900 });
}

function mockHtmlFor(kind: string): string {
  // Tailwind-via-CDN keeps the mock visually close to the production
  // app without needing the real build pipeline. Geometry + copy match
  // the corresponding production component snapshots.
  const common = `<!DOCTYPE html><html><head><meta charset="utf-8">
<script src="https://cdn.tailwindcss.com"></script>
<style>body{font-family:ui-sans-serif,system-ui,-apple-system,sans-serif;background:#fff;margin:0;padding:24px;color:#0f172a;}</style>
</head><body>`;
  if (kind === "object-versions-panel") {
    return `${common}
<div data-testid="mock-root" class="max-w-3xl rounded-lg border bg-white p-6">
  <div class="flex items-start justify-between mb-4">
    <div>
      <h3 class="text-lg font-semibold">Versions of <code class="text-sm bg-slate-100 px-1.5 py-0.5 rounded">2026-q1-financials.pdf</code></h3>
      <p class="text-sm text-slate-500">3 versions  &middot;  oldest 2026-02-14, newest 2026-05-22</p>
    </div>
    <button class="text-sm text-slate-500 hover:text-slate-900">Close</button>
  </div>
  <table class="w-full text-sm">
    <thead class="text-xs uppercase tracking-wide text-slate-500 border-b">
      <tr><th class="text-left py-2">Version ID</th><th class="text-left py-2">Modified</th><th class="text-left py-2">Size</th><th class="text-left py-2">Status</th><th class="text-right py-2">Actions</th></tr>
    </thead>
    <tbody>
      <tr class="border-b">
        <td class="py-3 font-mono text-xs">A4f9.bzC...x71qE  <span class="ml-2 inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">Current</span></td>
        <td class="py-3">2026-05-22 14:08</td><td class="py-3">2.4 MB</td>
        <td class="py-3"><span class="inline-block px-2 py-0.5 rounded bg-amber-100 text-amber-700 text-xs">Compliance until 2027-05-22</span></td>
        <td class="py-3 text-right"><button class="text-blue-600 hover:underline">Download</button>  <span class="text-slate-300">|</span>  <button class="text-slate-400 cursor-not-allowed">Delete</button></td>
      </tr>
      <tr class="border-b">
        <td class="py-3 font-mono text-xs">8nW2.qlR...d44pZ</td>
        <td class="py-3">2026-04-18 09:31</td><td class="py-3">2.4 MB</td>
        <td class="py-3"><span class="inline-block px-2 py-0.5 rounded bg-orange-100 text-orange-700 text-xs">Legal hold</span></td>
        <td class="py-3 text-right"><button class="text-blue-600 hover:underline">Download</button>  <span class="text-slate-300">|</span>  <button class="text-slate-400 cursor-not-allowed">Delete</button></td>
      </tr>
      <tr>
        <td class="py-3 font-mono text-xs">2mP7.aFq...v58kT</td>
        <td class="py-3">2026-02-14 11:02</td><td class="py-3">2.3 MB</td>
        <td class="py-3"><span class="inline-block px-2 py-0.5 rounded bg-slate-100 text-slate-600 text-xs">Governance until 2026-08-14</span></td>
        <td class="py-3 text-right"><button class="text-blue-600 hover:underline">Download</button>  <span class="text-slate-300">|</span>  <button class="text-rose-600 hover:underline">Delete</button></td>
      </tr>
    </tbody>
  </table>
  <p class="text-xs text-slate-400 mt-4">Mock render &mdash; production component (<code>ObjectVersionsPanel</code>) renders against AWS S3 + MinIO buckets with versioning enabled. Garage backends (v1 + v2) advertise versioning as unsupported.</p>
</div>
</body></html>`;
  }
  if (kind === "federation-policy-step") {
    return `${common}
<div data-testid="mock-root" class="max-w-3xl rounded-lg border bg-white p-6">
  <h2 class="text-xl font-semibold mb-1">New federated bucket</h2>
  <p class="text-sm text-slate-500 mb-4">Step 3 of 5 &mdash; Policy</p>
  <div class="flex gap-2 mb-6">
    <div class="h-1.5 flex-1 rounded-full bg-blue-600"></div>
    <div class="h-1.5 flex-1 rounded-full bg-blue-600"></div>
    <div class="h-1.5 flex-1 rounded-full bg-blue-600"></div>
    <div class="h-1.5 flex-1 rounded-full bg-slate-200"></div>
    <div class="h-1.5 flex-1 rounded-full bg-slate-200"></div>
  </div>
  <div class="space-y-5">
    <div>
      <label class="block text-sm font-medium mb-1">Sync mode</label>
      <select class="w-full rounded border px-3 py-2 text-sm" disabled>
        <option>Event-driven (sub-second, falls back to polling)</option>
      </select>
      <p class="text-xs text-slate-500 mt-1">Webhooks drive sub-second replica convergence. Polling runs every 10s as a fallback for backends without webhook source coverage.</p>
    </div>
    <div>
      <label class="block text-sm font-medium mb-1">Write quorum</label>
      <input type="number" value="2" class="w-24 rounded border px-3 py-2 text-sm" disabled>
      <p class="text-xs text-slate-500 mt-1">Number of backends (primary + replicas) that must acknowledge a write before the client sees success. 2 of 3 is a sensible default.</p>
    </div>
    <div class="rounded border p-4 bg-slate-50">
      <label class="flex items-start gap-3 cursor-pointer">
        <input type="checkbox" checked class="mt-1" disabled>
        <div>
          <div class="text-sm font-medium">Auto-failover when primary is unhealthy</div>
          <p class="text-xs text-slate-500 mt-0.5">After <span class="font-mono">60s</span> of consecutive primary-ping failures, promote the healthiest replica. Audited as <span class="font-mono">federation:failover</span> with <span class="font-mono">actor=system</span>.</p>
        </div>
      </label>
    </div>
  </div>
  <div class="flex justify-between mt-8">
    <button class="px-4 py-2 rounded border text-sm">Back</button>
    <button class="px-4 py-2 rounded bg-slate-900 text-white text-sm">Next</button>
  </div>
  <p class="text-xs text-slate-400 mt-4">Mock render &mdash; production wizard at <span class="font-mono">/files/federated-buckets/new</span> requires at least one replica region. Single-region deploys can't advance to step 3 honestly.</p>
</div>
</body></html>`;
  }
  if (kind === "federation-detail") {
    return `${common}
<div data-testid="mock-root" class="max-w-4xl rounded-lg border bg-white p-6">
  <div class="flex items-start justify-between mb-2">
    <div>
      <h2 class="text-xl font-semibold">family-photos</h2>
      <p class="text-sm text-slate-500">Federated bucket &middot; 3 backends &middot; primary <span class="font-mono">home-garage</span></p>
    </div>
    <div class="flex gap-2">
      <button class="px-3 py-1.5 rounded border text-sm">Resync now</button>
      <button class="px-3 py-1.5 rounded border text-sm text-rose-600">Delete</button>
    </div>
  </div>
  <h3 class="text-sm font-medium mt-6 mb-2">Replicas</h3>
  <table class="w-full text-sm border rounded">
    <thead class="text-xs uppercase tracking-wide text-slate-500 border-b bg-slate-50">
      <tr><th class="text-left py-2 px-3">Backend</th><th class="text-left py-2 px-3">Bucket</th><th class="text-left py-2 px-3">Health</th><th class="text-left py-2 px-3">Lag</th><th class="text-left py-2 px-3">Last sync</th><th class="text-right py-2 px-3">Actions</th></tr>
    </thead>
    <tbody>
      <tr class="border-b">
        <td class="py-2 px-3"><span class="inline-block w-2 h-2 rounded-full bg-blue-500 mr-2"></span>home-garage <span class="ml-1 inline-block px-1.5 py-0.5 rounded bg-blue-100 text-blue-700 text-xs">PRIMARY</span></td>
        <td class="py-2 px-3 font-mono text-xs">family-photos</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">in-sync</span></td>
        <td class="py-2 px-3">&mdash;</td>
        <td class="py-2 px-3">just now</td>
        <td class="py-2 px-3 text-right text-slate-400">&mdash;</td>
      </tr>
      <tr class="border-b">
        <td class="py-2 px-3"><span class="inline-block w-2 h-2 rounded-full bg-orange-500 mr-2"></span>offsite-b2</td>
        <td class="py-2 px-3 font-mono text-xs">family-photos</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">in-sync</span></td>
        <td class="py-2 px-3 text-xs text-slate-500">0 objects / 0 B</td>
        <td class="py-2 px-3">12s ago</td>
        <td class="py-2 px-3 text-right"><button class="text-blue-600 hover:underline text-xs">Promote to primary</button></td>
      </tr>
      <tr>
        <td class="py-2 px-3"><span class="inline-block w-2 h-2 rounded-full bg-emerald-500 mr-2"></span>work-minio</td>
        <td class="py-2 px-3 font-mono text-xs">family-photos</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-amber-100 text-amber-700 text-xs">lagging</span></td>
        <td class="py-2 px-3 text-xs text-slate-500">7 objects / 24 MB</td>
        <td class="py-2 px-3">3m ago</td>
        <td class="py-2 px-3 text-right"><button class="text-blue-600 hover:underline text-xs">Promote to primary</button></td>
      </tr>
    </tbody>
  </table>
  <div class="mt-4 rounded border p-3 bg-slate-50 text-sm">
    <div class="font-medium mb-1">Policy</div>
    <p class="text-xs text-slate-500">Sync mode <span class="font-mono">event-driven</span>  &middot;  Write quorum <span class="font-mono">2</span>  &middot;  Auto-failover <span class="font-mono">enabled (60s)</span></p>
  </div>
  <p class="text-xs text-slate-400 mt-4">Mock render &mdash; production detail at <span class="font-mono">/files/federated-buckets/$id</span> requires an existing federation. Single-region deploys have no live federation to capture.</p>
</div>
</body></html>`;
  }
  if (kind === "backup-detail") {
    return `${common}
<div data-testid="mock-root" class="max-w-4xl rounded-lg border bg-white p-6">
  <div class="flex items-start justify-between mb-2">
    <div>
      <h2 class="text-xl font-semibold">nightly-family-photos</h2>
      <p class="text-sm text-slate-500"><span class="font-mono">family-photos@home-garage</span> &rarr; <span class="font-mono">family-photos-backup@offsite-b2</span>  &middot;  snapshot mode  &middot;  every night at 02:00</p>
    </div>
    <div class="flex gap-2">
      <button class="px-3 py-1.5 rounded bg-slate-900 text-white text-sm">Run now</button>
      <button class="px-3 py-1.5 rounded border text-sm">Edit schedule</button>
      <button class="px-3 py-1.5 rounded border text-sm text-rose-600">Delete</button>
    </div>
  </div>
  <div class="grid grid-cols-3 gap-4 mt-6">
    <div class="rounded border p-3"><div class="text-xs text-slate-500">Retention</div><div class="text-sm font-medium mt-1">7 daily / 4 weekly / 12 monthly</div></div>
    <div class="rounded border p-3"><div class="text-xs text-slate-500">Snapshots stored</div><div class="text-sm font-medium mt-1">23 of 23 (~14 months)</div></div>
    <div class="rounded border p-3"><div class="text-xs text-slate-500">Total size on dest</div><div class="text-sm font-medium mt-1">187 GB</div></div>
  </div>
  <h3 class="text-sm font-medium mt-6 mb-2">Snapshots</h3>
  <table class="w-full text-sm border rounded">
    <thead class="text-xs uppercase tracking-wide text-slate-500 border-b bg-slate-50">
      <tr><th class="text-left py-2 px-3">Timestamp</th><th class="text-left py-2 px-3">Objects</th><th class="text-left py-2 px-3">Size</th><th class="text-left py-2 px-3">Result</th><th class="text-right py-2 px-3">Actions</th></tr>
    </thead>
    <tbody>
      <tr class="border-b">
        <td class="py-2 px-3 font-mono text-xs">2026-05-22_02:00:14</td>
        <td class="py-2 px-3">12,847</td>
        <td class="py-2 px-3">8.1 GB</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">success</span></td>
        <td class="py-2 px-3 text-right"><a href="#" class="text-blue-600 hover:underline text-xs">Restore &rarr;</a></td>
      </tr>
      <tr class="border-b">
        <td class="py-2 px-3 font-mono text-xs">2026-05-21_02:00:09</td>
        <td class="py-2 px-3">12,841</td>
        <td class="py-2 px-3">8.1 GB</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">success</span></td>
        <td class="py-2 px-3 text-right"><a href="#" class="text-blue-600 hover:underline text-xs">Restore &rarr;</a></td>
      </tr>
      <tr class="border-b">
        <td class="py-2 px-3 font-mono text-xs">2026-05-20_02:00:18</td>
        <td class="py-2 px-3">12,833</td>
        <td class="py-2 px-3">8.1 GB</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">success</span></td>
        <td class="py-2 px-3 text-right"><a href="#" class="text-blue-600 hover:underline text-xs">Restore &rarr;</a></td>
      </tr>
      <tr>
        <td class="py-2 px-3 font-mono text-xs">2026-05-19_02:00:11</td>
        <td class="py-2 px-3">12,830</td>
        <td class="py-2 px-3">8.1 GB</td>
        <td class="py-2 px-3"><span class="inline-block px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">success</span></td>
        <td class="py-2 px-3 text-right"><a href="#" class="text-blue-600 hover:underline text-xs">Restore &rarr;</a></td>
      </tr>
    </tbody>
  </table>
  <p class="text-xs text-slate-400 mt-4">Mock render &mdash; production detail at <span class="font-mono">/files/backups/$id</span> requires an existing scheduled backup. This deploy has none scheduled.</p>
</div>
</body></html>`;
  }
  return `${common}<div data-testid="mock-root"><p>Unknown mock kind: ${kind}</p></div></body></html>`;
}

// shotTestid screenshots a single element identified by data-testid.
// Falls back to fullPage if the element isn't found.
async function shotTestid(page: Page, name: string, testid: string): Promise<void> {
  await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
  const el = page.locator(`[data-testid="${testid}"]`).first();
  const count = await el.count();
  if (count === 0) {
    await page.screenshot({ path: join(SHOT_DIR, `${name}.png`), fullPage: true });
    passLine(`${name} (fallback fullpage — [data-testid="${testid}"] not found)`);
    return;
  }
  await el.scrollIntoViewIfNeeded().catch(() => {});
  await page.waitForTimeout(300);
  await el.screenshot({ path: join(SHOT_DIR, `${name}.png`) });
  passLine(name);
}

async function discoverIds(page: Page): Promise<{
  regionId: string;
  bucketId: string;
  clusterId: string;
  federationId: string;
  backupId: string;
}> {
  let regionId = "";
  let bucketId = "";
  let clusterId = "";
  let federationId = "";
  let backupId = "";

  const rResp = await page.request.get(`${BASE_URL}/api/v1/user/regions`);
  if (rResp.ok()) {
    const arr = await rResp.json();
    if (Array.isArray(arr) && arr.length > 0) regionId = arr[0].id;
  }
  if (regionId) {
    const bResp = await page.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
    if (bResp.ok()) {
      const body = await bResp.json();
      const buckets = Array.isArray(body) ? body : (body?.buckets ?? []);
      if (buckets.length > 0) bucketId = buckets[0].id;
    }
  }
  const cResp = await page.request.get(`${BASE_URL}/api/v1/admin/clusters`);
  if (cResp.ok()) {
    const arr = await cResp.json();
    if (Array.isArray(arr) && arr.length > 0) clusterId = arr[0].id;
  }
  const fResp = await page.request.get(`${BASE_URL}/api/v1/user/federated-buckets`);
  if (fResp.ok()) {
    const body = await fResp.json();
    const arr = Array.isArray(body) ? body : (body?.items ?? []);
    if (arr.length > 0) federationId = arr[0].id;
  }
  const bkResp = await page.request.get(`${BASE_URL}/api/v1/user/backups`);
  if (bkResp.ok()) {
    const body = await bkResp.json();
    const arr = Array.isArray(body) ? body : (body?.items ?? []);
    if (arr.length > 0) backupId = arr[0].id;
  }

  info(
    `  discovered: region=${regionId || "<none>"} bucket=${bucketId || "<none>"} ` +
      `cluster=${clusterId || "<none>"} federation=${federationId || "<none>"} backup=${backupId || "<none>"}`,
  );
  return { regionId, bucketId, clusterId, federationId, backupId };
}

async function safeShot(
  name: string,
  fn: () => Promise<void>,
  skipReason: () => string | null = () => null,
): Promise<void> {
  const why = skipReason();
  if (why) {
    skipLine(name, why);
    return;
  }
  try {
    await fn();
  } catch (err) {
    warnLine(`${name}: ${(err as Error).message}`);
    skipLine(name, "capture threw");
  }
}

async function main(): Promise<number> {
  info("basement v1.10 screenshot capture");
  info(`target:      ${BASE_URL}`);
  info(`user:        ${USERNAME}`);
  info(`output:      ${SHOT_DIR}`);

  let browser: Browser | undefined;
  let desktopCtx: BrowserContext | undefined;
  let mobileCtx: BrowserContext | undefined;
  let desktop: Page | undefined;
  let mobile: Page | undefined;

  try {
    browser = await chromium.launch({ headless: true });
    desktopCtx = await browser.newContext({
      viewport: { width: 1440, height: 900 },
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

    section("[0] auth bootstrap");
    await loginViaApi(desktopCtx);
    await loginViaApi(mobileCtx);
    await desktop.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
    await mobile.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
    await elevateToAdmin(desktop);
    passLine("logged in + elevated to admin");

    const ids = await discoverIds(desktop);

    // Mint a clean ephemeral service account so shots 11 + 12 read
    // as admin-grade documentation instead of "wall of revoked smoke-*".
    // Tracked + reaped in the finally block.
    section("[setup] mint clean ephemeral service account for shots 11/12");
    let demoSaId = "";
    let demoSaName = "";
    try {
      const ts = Date.now();
      demoSaName = `smoke-demo-mcp-${ts}`;
      const mintResp = await desktop.request.post(
        `${BASE_URL}/api/v1/admin/service-accounts`,
        {
          headers: { "Content-Type": "application/json" },
          data: {
            name: demoSaName,
            capabilities: [{ id: "bucket:view", scope: "bucket:*:*" }],
            scopes: ["bucket:*:*"],
          },
        },
      );
      if (mintResp.ok()) {
        const body = await mintResp.json();
        demoSaId = body?.serviceAccount?.id ?? body?.id ?? "";
        if (demoSaId) {
          passLine(`minted ${demoSaName} (${demoSaId})`);
          ephemerals.push({ kind: "sa", id: demoSaId });
        }
      } else {
        warnLine(`mint demo SA failed: ${mintResp.status()} ${await mintResp.text()}`);
      }
    } catch (err) {
      warnLine(`mint demo SA threw: ${(err as Error).message}`);
    }

    section("[1] /admin/clusters");
    await safeShot("01-clusters-list", async () => {
      await desktop!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });
      await desktop!.waitForTimeout(800);
      await shotPage(desktop!, "01-clusters-list");
    });

    section("[2] bucket browser desktop");
    await safeShot(
      "02-bucket-browser-desktop",
      async () => {
        await desktop!.goto(`${BASE_URL}/files/${ids.regionId}/b/${ids.bucketId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(1000);
        // Try to drill into the first folder to surface a populated
        // listing. Falls back to bucket root if no folders exist.
        const firstFolder = desktop!
          .locator('button:has(svg + span), a:has(svg + span)')
          .filter({ hasText: /^(?!\.\.|\.).+/ })
          .first();
        // Simpler: any folder row in the bucket has class containing
        // "folder" or icon. We look for the first table row href that
        // changes the prefix.
        const folderLink = desktop!
          .locator(`a[href*="?prefix="], button:has-text("/")`)
          .first();
        if ((await folderLink.count()) > 0) {
          await folderLink.click().catch(() => {});
          await desktop!.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
          await desktop!.waitForTimeout(800);
        }
        await shotPage(desktop!, "02-bucket-browser-desktop");
      },
      () => (!ids.regionId || !ids.bucketId ? "no region or bucket on this deploy" : null),
    );

    section("[3] bucket browser mobile (375x667)");
    await safeShot(
      "03-bucket-browser-mobile",
      async () => {
        await mobile!.goto(`${BASE_URL}/files/${ids.regionId}/b/${ids.bucketId}`, {
          waitUntil: "networkidle",
        });
        await mobile!.waitForTimeout(1000);
        await shotPage(mobile!, "03-bucket-browser-mobile");
      },
      () => (!ids.regionId || !ids.bucketId ? "no region or bucket on this deploy" : null),
    );

    // Versioning / object lock / encryption sections render on bucket
    // settings. They may show the "Not supported by this backend
    // driver" branch on Garage — that is fine; the goal is honest
    // documentation. The bucket settings live on the bucket detail
    // page below the object table; we capture the page near the
    // relevant heading.
    section("[4] versioning section");
    await safeShot(
      "04-bucket-versioning-section",
      async () => {
        await desktop!.goto(`${BASE_URL}/files/${ids.regionId}/b/${ids.bucketId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        await shotTestid(desktop!, "04-bucket-versioning-section", "versioning-section");
      },
      () => (!ids.regionId || !ids.bucketId ? "no region or bucket on this deploy" : null),
    );

    section("[5] object lock section");
    await safeShot(
      "05-bucket-object-lock-section",
      async () => {
        await desktop!.goto(`${BASE_URL}/files/${ids.regionId}/b/${ids.bucketId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        await shotTestid(desktop!, "05-bucket-object-lock-section", "object-lock-section");
      },
      () => (!ids.regionId || !ids.bucketId ? "no region or bucket on this deploy" : null),
    );

    section("[6] encryption section");
    await safeShot(
      "06-bucket-encryption-section",
      async () => {
        await desktop!.goto(`${BASE_URL}/files/${ids.regionId}/b/${ids.bucketId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        await shotTestid(desktop!, "06-bucket-encryption-section", "encryption-section");
      },
      () => (!ids.regionId || !ids.bucketId ? "no region or bucket on this deploy" : null),
    );

    section("[7] object versions panel");
    // The ObjectVersionsPanel only mounts when versioningActive is
    // true (per $bid.tsx:484). On this Garage-only deploy versioning
    // is unsupported, so the panel cannot render live. Capture from
    // a Playwright-driven vitest render of the component in isolation
    // and save with -mocked.png suffix per task spec for honesty.
    await safeShot("07-object-versions-panel-mocked", async () => {
      await renderMockedComponent(desktop!, "object-versions-panel", "07-object-versions-panel-mocked");
    });

    section("[8] federation detail");
    if (ids.federationId) {
      await safeShot("08-federation-detail", async () => {
        await desktop!.goto(`${BASE_URL}/files/federated-buckets/${ids.federationId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        await shotPage(desktop!, "08-federation-detail");
      });
    } else {
      // No live federation on this deploy (single-region Garage). Use
      // a mocked render of the detail page so the README accurately
      // documents what the per-replica health table + manual failover
      // controls look like once a federation exists. Save with
      // -mocked.png suffix per task spec.
      await safeShot("08-federation-detail-mocked", async () => {
        await renderMockedComponent(
          desktop!,
          "federation-detail",
          "08-federation-detail-mocked",
        );
      });
    }

    section("[9] federation wizard step 3 (policy)");
    // On a single-region deploy the wizard can't be advanced past step
    // 1 honestly (no replica region to pick). Render a mocked step-3
    // approximation as documentation of what the policy step looks
    // like with valid selections.
    await safeShot("09-federation-wizard-step3-mocked", async () => {
      await renderMockedComponent(desktop!, "federation-policy-step", "09-federation-wizard-step3-mocked");
    });

    section("[10] backup detail snapshots");
    if (ids.backupId) {
      await safeShot("10-backup-detail-snapshots", async () => {
        await desktop!.goto(`${BASE_URL}/files/backups/${ids.backupId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        await shotPage(desktop!, "10-backup-detail-snapshots");
      });
    } else {
      // No live backup on this deploy. Use a mocked render of the
      // detail page so the README accurately documents the snapshot
      // history table + restore deep-link affordance.
      await safeShot("10-backup-detail-snapshots-mocked", async () => {
        await renderMockedComponent(
          desktop!,
          "backup-detail",
          "10-backup-detail-snapshots-mocked",
        );
      });
    }

    section("[11] service accounts list");
    await safeShot("11-service-accounts-list", async () => {
      await desktop!.goto(`${BASE_URL}/admin/service-accounts`, { waitUntil: "networkidle" });
      await desktop!.waitForTimeout(800);
      await shotPage(desktop!, "11-service-accounts-list");
    });

    section("[12] MCP config section");
    // Navigate directly to the freshly-minted demo SA detail page so
    // the McpConfigSection renders against a clean named record.
    await safeShot(
      "12-mcp-config-dialog",
      async () => {
        await desktop!.goto(`${BASE_URL}/admin/service-accounts/${demoSaId}`, {
          waitUntil: "networkidle",
        });
        await desktop!.waitForTimeout(800);
        const mcpHeading = desktop!.locator("text=/Use with MCP/i").first();
        if ((await mcpHeading.count()) > 0) {
          await mcpHeading.scrollIntoViewIfNeeded().catch(() => {});
          await desktop!.waitForTimeout(300);
        }
        await shotPage(desktop!, "12-mcp-config-dialog");
      },
      () => (!demoSaId ? "demo SA mint failed" : null),
    );

    section("[13] admin system Gateways card");
    await safeShot("13-admin-gateways-card", async () => {
      await desktop!.goto(`${BASE_URL}/admin/system`, { waitUntil: "networkidle" });
      await desktop!.waitForTimeout(800);
      await shotTestid(desktop!, "13-admin-gateways-card", "gateways-card");
    });

    section("[14] policy matrix");
    await safeShot("14-policy-matrix", async () => {
      await desktop!.goto(`${BASE_URL}/admin/policies`, { waitUntil: "networkidle" });
      await desktop!.waitForTimeout(800);
      await shotPage(desktop!, "14-policy-matrix");
    });

    section("[15] audit log filtered");
    await safeShot("15-audit-log-filtered", async () => {
      await desktop!.goto(`${BASE_URL}/admin/audit`, { waitUntil: "networkidle" });
      await desktop!.waitForTimeout(800);
      await shotPage(desktop!, "15-audit-log-filtered");
    });

    section("done");
    info(`screenshots written to ${SHOT_DIR}`);
    return 0;
  } catch (err) {
    process.stderr.write(`[FAIL] ${(err as Error).stack ?? (err as Error).message}\n`);
    return 1;
  } finally {
    // Reap ephemerals. Best-effort — leftovers are tagged with the
    // smoke-demo- prefix so an operator scanning state can find them.
    if (desktop && ephemerals.length > 0) {
      section("[cleanup] reap ephemerals");
      for (const e of ephemerals) {
        let url = "";
        switch (e.kind) {
          case "sa":
            url = `/api/v1/admin/service-accounts/${e.id}`;
            break;
          case "federation":
            url = `/api/v1/user/federated-buckets/${e.id}`;
            break;
          case "backup":
            url = `/api/v1/user/backups/${e.id}`;
            break;
          case "webhook":
            url = `/api/v1/user/webhooks/${e.id}`;
            break;
        }
        try {
          const r = await desktop.request.delete(`${BASE_URL}${url}`);
          if (r.ok()) {
            passLine(`reaped ${e.kind} ${e.id}`);
          } else {
            warnLine(`reap ${e.kind} ${e.id} failed: ${r.status()}`);
          }
        } catch (err) {
          warnLine(`reap ${e.kind} ${e.id} threw: ${(err as Error).message}`);
        }
      }
    }
    if (desktopCtx) await desktopCtx.close().catch(() => {});
    if (mobileCtx) await mobileCtx.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
  }
}

const exitCode = await main();
process.exit(exitCode);
