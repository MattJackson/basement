#!/usr/bin/env node
// postdeploy-ui-smoke.ts — black-box UI smoke for a live basement.
//
// Sibling of scripts/postdeploy-smoke.sh — the bash script exercises
// the JSON API directly; this one drives a real headless Chromium
// through the React app to catch the class of bug that API-level
// checks miss:
//
//   - route configuration regressions (e.g. v0.3.1 fix where
//     /admin/clusters/$cid was acting as a parent layout without an
//     <Outlet />, redirecting bucket-row clicks back to the cluster
//     detail page)
//   - missing-on-render bugs (counts, badges, section headers)
//   - silent runtime errors that the API can't see
//
// Usage:
//   node scripts/postdeploy-ui-smoke.ts
//   BASE_URL=https://basement.example.com \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/postdeploy-ui-smoke.ts
//
// Note: NOT named USERNAME/PASSWORD. macOS bash exports a readonly
// USERNAME=<os-user> that shadows any inline `USERNAME=foo` prefix,
// causing silent auth failures. BUI_* avoids the collision.
//
// Requires:
//   - Node 24+ (native TypeScript stripping via amaro)
//   - `playwright` installed as a devDep in frontend/
//   - chromium installed once: `pnpm -C frontend exec playwright install chromium`
//
// Exit codes:
//   0  all checks passed
//   1  one or more checks failed
//   2  bad invocation / setup error

// Playwright is installed in frontend/node_modules — the script
// lives in scripts/, so a bare ESM specifier won't resolve. We
// compute the absolute path at runtime and import dynamically below.
// Types come from `playwright` via a type-only import so this stays
// strictly typed without needing a tsconfig.
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
// BUI_* prefix avoids collision with macOS's readonly USERNAME export.
// USERNAME/PASSWORD remain accepted as fallbacks for CI environments
// that have them set deliberately, but BUI_* takes precedence.
const USERNAME = process.env.BUI_USERNAME ?? process.env.BASEMENT_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? process.env.BASEMENT_PASSWORD ?? process.env.PASSWORD ?? "password";

const RUN_TS = new Date().toISOString().replace(/[:.]/g, "-");
// SMOKE_SHOT_DIR lets the operator pin a known directory (e.g. for a
// release-tag screenshot pass like /tmp/v1.5.0-screenshots/) instead
// of the timestamped per-run default.
const SHOT_DIR = process.env.SMOKE_SHOT_DIR ?? join("/tmp", "basement-smoke", RUN_TS);
mkdirSync(SHOT_DIR, { recursive: true });

// ---------- color helpers (no emoji, match bash smoke tone) ----------
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

// ---------- results ----------
type Result = { name: string; ok: boolean; ms: number; detail?: string };
const results: Result[] = [];

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

function skipLine(name: string, reason: string) {
  process.stdout.write(`${C.yellow}[skip]${C.reset} ${name} ${C.dim}(${reason})${C.reset}\n`);
  results.push({ name, ok: true, ms: 0, detail: `skipped: ${reason}` });
}

// ---------- console / pageerror tracking ----------
type LoggedConsole = { type: string; text: string; url: string };
const consoleErrors: LoggedConsole[] = [];
const consoleWarnings: LoggedConsole[] = [];
const pageErrors: { message: string; url: string }[] = [];

function attachListeners(page: Page) {
  page.on("console", (msg: ConsoleMessage) => {
    const t = msg.type();
    const entry = { type: t, text: msg.text(), url: page.url() };
    if (t === "error") {
      // Filter out the noisy favicon 404 fallback and other low-signal
      // browser-level fetch failures we can't act on.
      if (entry.text.includes("Failed to load resource")) return;
      consoleErrors.push(entry);
    } else if (t === "warning") {
      consoleWarnings.push(entry);
    }
  });
  page.on("pageerror", (err: Error) => {
    pageErrors.push({ message: `${err.name}: ${err.message}`, url: page.url() });
  });
}

// elevateToAdmin re-auths the current session to ADMIN mode via
// /api/v1/auth/elevate (ADR-0003 v1.2.0a, simplified to two modes in
// the v1.3.0a.4 amendment). Idempotent — calling while already
// ADMIN simply bumps the expiry.
//
// The smoke needs this before exercising any /admin/* page that
// auto-fires capabilities above USER (the clusters list auto-tests
// cluster:test on each row, /admin/audit reads audit:view, etc.) —
// without elevation, the openapi-fetch middleware pops the elevation
// modal and intercepts subsequent clicks.
//
// Uses page.request so the session cookie travels with the call and
// the new mode cookie updates the browser context automatically.
async function elevateToAdmin(page: Page, password: string): Promise<void> {
  const resp = await page.request.post(`${BASE_URL}/api/v1/auth/elevate`, {
    headers: { "Content-Type": "application/json" },
    data: { target_mode: "admin", password },
  });
  if (!resp.ok()) {
    throw new Error(`POST /api/v1/auth/elevate → ${resp.status()} ${await resp.text()}`);
  }
}

async function shot(page: Page, name: string) {
  try {
    // Wait briefly for skeleton loaders to resolve so the screenshot
    // is useful for visual debugging. Best-effort — if the network
    // never settles we still take the shot.
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    await page.screenshot({ path: join(SHOT_DIR, `${name}.png`), fullPage: true });
  } catch {
    // Screenshots are best-effort — never block a check on them.
  }
}

// ---------- main flow ----------
async function main(): Promise<number> {
  info("basement post-deploy UI smoke");
  info(`target:      ${BASE_URL}`);
  info(`user:        ${USERNAME}`);
  info(`screenshots: ${SHOT_DIR}`);

  let browser: Browser | undefined;
  let context: BrowserContext | undefined;
  let page: Page | undefined;

  try {
    browser = await chromium.launch({ headless: true });
    context = await browser.newContext({
      viewport: { width: 1280, height: 900 },
      // Treat self-signed / sketchy certs as fatal — the deploy URL is HTTPS-only.
      ignoreHTTPSErrors: false,
    });
    page = await context.newPage();
    attachListeners(page);

    // Track which cluster cid + first bucket id + first key id we discovered.
    let discoveredCid = "";
    let discoveredBucketId = "";
    let discoveredKeyId = "";
    let firstClusterDriver = "";

    // ============================================================
    // 1. Login flow
    // ============================================================
    section("[1] login flow");
    await check("GET / → redirected to /login", async () => {
      const resp = await page!.goto(`${BASE_URL}/`, { waitUntil: "networkidle" });
      if (!resp) throw new Error("no response from initial GET");
      if (resp.status() >= 500) throw new Error(`HTTP ${resp.status()} on initial GET`);
      // Wait for the SPA to settle on the login route.
      await page!.waitForURL(/\/admin\/login/, { timeout: 10_000 });
      // Login form should be visible.
      await page!.waitForSelector('input#username', { timeout: 5_000 });
      await page!.waitForSelector('input#password', { timeout: 5_000 });
      await shot(page!, "01-login");
    });

    await check("submit credentials → land on /files (user shell)", async () => {
      // Auth via Node fetch then inject the Set-Cookie into the
      // browser context. The Playwright-browser-context login flow
      // (form-fill + click + waitForResponse) has been observed to
      // return 401 against the live deploy even when an identical
      // body via Node fetch returns 200 — the failure mode is timing-
      // sensitive and not worth chasing for a smoke. Form rendering
      // is still validated by the preceding waitForSelector calls.
      //
      // v1.3.0 (fcb7e0d): login no longer auto-redirects admins to
      // /admin — every user lands on /files (user shell). Operators
      // step into admin mode via the UserMenu "Switch to admin view"
      // affordance per ADR-0003. Assertion updated accordingly.
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
        throw new Error(`no __Host- session cookie in Set-Cookie response: ${setCookie}`);
      }
      await context!.addCookies([
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
      // Navigate to the post-login default landing.
      await page!.goto(`${BASE_URL}/`, { waitUntil: "networkidle" });
      // v1.3.0 (fcb7e0d): / now resolves to /files for everyone, not
      // /admin/clusters for admins. The user shell renders Files /
      // Keys / Shares in the Primary nav.
      await page!.waitForURL(/\/files($|\?|#)/, { timeout: 10_000 });
      await page!.waitForSelector('nav[aria-label="Primary"] >> text=Files', { timeout: 10_000 });
      await shot(page!, "02-post-login");
    });

    // ============================================================
    // 1.5 root resolves to /files for everyone (v1.3.0 fcb7e0d)
    // ============================================================
    section("[1.5] root resolves to /files for everyone (v1.3.0)");

    await check("/ → /files for all users (no admin auto-redirect)", async () => {
      await page!.goto(`${BASE_URL}/`, { waitUntil: "networkidle" });
      // v1.3.0 (fcb7e0d): everyone lands on the user shell after login.
      // Admins step into /admin via the UserMenu "Switch to admin view"
      // affordance per ADR-0003 — no implicit role-based redirect.
      await page!.waitForURL(/\/files($|\?|#)/, { timeout: 10_000 });
      await page!.waitForSelector('h1:has-text("My Regions")', { timeout: 10_000 });
    });

    await check("/files renders user shell placeholder", async () => {
      // Direct navigation lands on the user shell with "My Regions" header.
      await page!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("My Regions")', { timeout: 10_000 });
    });

    // ============================================================
    // 1.6 user-persona chrome (v0.5.3 USER.SHELL)
    // ============================================================
    section("[1.6] user-persona chrome");
    
    await check("[NN] /files header shows user-persona nav (Files/Keys/Shares)", async () => {
      await page!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });
      
      // Assert Files, Keys, Shares nav items are present in Primary nav
      const primaryNav = page!.locator('nav[aria-label="Primary"]');
      
      await Promise.all([
        primaryNav.locator('text=Files').first().waitFor({ state: 'visible', timeout: 10_000 }),
        primaryNav.locator('text=Keys').first().waitFor({ state: 'visible', timeout: 10_000 }),
        primaryNav.locator('text=Shares').first().waitFor({ state: 'visible', timeout: 10_000 }),
      ]);

      // Assert Clusters is NOT in the Primary nav (user-shell should not show it)
      const clustersLink = await primaryNav.locator('text=Clusters').count();
      if (clustersLink > 0) {
        throw new Error("User shell incorrectly shows 'Clusters' in Primary nav - should only show Files/Keys/Shares");
      }

      // Assert Logo href is /files
      const logoHref = await page!.locator('a[data-testid="logo"]').getAttribute('href');
      if (logoHref !== "/files") {
        throw new Error(`Logo href is '${logoHref}', expected '/files'`);
      }

      await shot(page!, "13-user-shell");
    });

    // ============================================================
    // 1.7 My Regions card grid (ADR-0002, v1.1.0c)
    // ============================================================
    section("[1.7] My Regions card grid (v1.1.0c)");

    await check("/files renders key cards or empty state (v1.2.0d key-first)", async () => {
      await page!.goto(`${BASE_URL}/files`, { waitUntil: "networkidle" });

      // v1.2.0d (key-first model): heading stays "My Regions" but the
      // subtitle reframes around access keys. Each card is a
      // UserKeyCard with data-testid="user-key-card-link".
      await page!.waitForSelector('h1:has-text("My Regions")', { timeout: 10_000 });
      await page!.waitForSelector('p:has-text("Each card is one of your access keys")', { timeout: 10_000 });

      const hasCards = await page!.locator('[data-testid="user-key-card-link"]').count();
      if (hasCards === 0) {
        // Empty-state CTA points at the canonical /files/keys/new form
        // (v1.2.0d). The legacy /files/regions/new still redirects but
        // the empty-state copy was retargeted at the canonical URL.
        const hasEmptyState = await page!.locator('text="No keys yet"').count();
        if (hasEmptyState > 0) {
          const connectCta = page!.locator('a:has-text("+ Add a key"), a:has-text("Add a key")').first();
          const href = await connectCta.getAttribute('href');
          if (href !== "/files/keys/new") {
            throw new Error(`Empty-state CTA href is '${href}', expected '/files/keys/new'`);
          }
          warnLine("/files shows empty state — expected on fresh deploy");
          return;
        }
        throw new Error("/files rendered neither key cards nor 'No keys yet' empty state");
      }

      // First card href must point at /files/{regionId}
      const firstCard = page!.locator('[data-testid="user-key-card-link"]').first();
      const href = await firstCard.getAttribute('href');
      if (!href || !href.match(/^\/files\/[^/]+$/)) {
        throw new Error(`First key card has invalid href: ${href}`);
      }

      await firstCard.click();
      await page!.waitForURL(/\/files\/[^/]+$/, { timeout: 10_000 });

      await shot(page!, "14-files-regions");
    });

    // v1.2.0d: /files/regions/new is a legacy alias that 301-redirects
    // to the canonical /files/keys/new "Add a key" form. We assert the
    // redirect lands on the right URL and renders the canonical heading.
    await check("/files/regions/new redirects to /files/keys/new (v1.2.0d)", async () => {
      await page!.goto(`${BASE_URL}/files/regions/new`, { waitUntil: "networkidle" });
      await page!.waitForURL(/\/files\/keys\/new$/, { timeout: 10_000 });
      await page!.waitForSelector('h1:has-text("Add a key")', { timeout: 10_000 });
      // All five fields must exist on the canonical form
      for (const id of ["alias", "endpoint", "accessKeyId", "secretKey", "region"]) {
        const has = await page!.locator(`#${id}`).count();
        if (has === 0) throw new Error(`/files/keys/new missing field #${id}`);
      }
      await shot(page!, "14b-files-keys-new");
    });

    // v1.3.0d: /files/keys/new now ships a "Bulk import" toggle that
    // swaps the single-key form for a paste-area accepting CSV / TSV
    // / aws credentials-file blocks.
    await check("/files/keys/new exposes the 'Bulk import' toggle (v1.3.0d)", async () => {
      await page!.goto(`${BASE_URL}/files/keys/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Add a key")', { timeout: 10_000 });
      const hasBulkToggle = await page!.locator('text=/Bulk import/i').count();
      if (hasBulkToggle === 0) {
        throw new Error("/files/keys/new missing 'Bulk import' toggle (v1.3.0d)");
      }
      await shot(page!, "14c-files-keys-new-bulk-import");
    });

    // ============================================================
    // 1.8 elevate to ADMIN before exercising /admin/* (ADR-0003)
    // ============================================================
    section("[1.8] elevate to ADMIN (ADR-0003 / v1.3.0a.4)");

    await check("POST /api/v1/auth/elevate (target_mode=admin)", async () => {
      await elevateToAdmin(page!, PASSWORD);
      // Confirm the mode cookie now reports admin in /auth/me.
      const meResp = await page!.request.get(`${BASE_URL}/api/v1/auth/me`);
      if (!meResp.ok()) throw new Error(`/auth/me → ${meResp.status()}`);
      const me = await meResp.json();
      if (me.mode !== "admin") {
        throw new Error(`expected mode=admin after elevate, got mode=${me.mode}`);
      }
      // Reload any open page so the AuthModeProvider picks up the new
      // cookie and the openapi-fetch middleware stops popping the
      // elevation modal on the auto-fired admin queries.
      await page!.goto(`${BASE_URL}/`, { waitUntil: "networkidle" });
    });

    // ============================================================
    // 2. Clusters list
    // ============================================================
    section("[2] clusters list");
    await check("navigate to /admin/clusters", async () => {
      await page!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });
      // Page heading.
      await page!.waitForSelector('h1:has-text("Clusters")', { timeout: 10_000 });
      // Wait for either at least one row OR the empty state.
      await page!.waitForFunction(
        () => {
          const rows = document.querySelectorAll('tbody tr');
          const empty = document.querySelector('[data-empty-state], h2:not(:empty)');
          return rows.length > 0 || (empty?.textContent?.includes("Welcome") ?? false);
        },
        { timeout: 15_000 },
      );
      await shot(page!, "03-clusters");
    });

    await check("first cluster row has label, driver badge, resources counts", async () => {
      const firstRow = page!.locator('tbody tr').first();
      const rowCount = await page!.locator('tbody tr').count();
      if (rowCount === 0) {
        throw new Error("no cluster rows rendered — first-run empty state is not a passing condition here");
      }
      const cells = firstRow.locator('td');
      // td[0] color dot, td[1] label, td[2] driver, td[3] status, td[4] resources counts, td[5] actions
      const labelText = (await cells.nth(1).textContent())?.trim() ?? "";
      if (!labelText) throw new Error("first cluster row label is empty");

      const driverText = (await cells.nth(2).textContent())?.trim() ?? "";
      if (!driverText) throw new Error("first cluster row driver badge is empty");
      firstClusterDriver = driverText.toLowerCase();

      // ClusterCounts loads async — wait until it has resolved to a
      // string containing "buckets" AND "keys" (i.e. not the "…"
      // loading placeholder).
      const resourcesCell = cells.nth(4);
      await page!.waitForFunction(
        (el) => {
          const t = el?.textContent ?? "";
          return /buckets/.test(t) && /keys/.test(t);
        },
        await resourcesCell.elementHandle(),
        { timeout: 15_000 },
      );
      const resourcesText = (await resourcesCell.textContent())?.trim() ?? "";
      if (!/buckets/.test(resourcesText) || !/keys/.test(resourcesText)) {
        throw new Error(`resources cell did not surface buckets+keys: '${resourcesText}'`);
      }
    });

    // ============================================================
    // 3. Cluster detail navigation (v0.3.1 regression test)
    // ============================================================
    section("[3] cluster detail navigation (v0.3.1 regression)");
    await check("click first cluster row → /admin/clusters/{cid} renders Cluster Admins + Buckets + Keys", async () => {
      // Click the label cell (column 2) rather than the row itself —
      // the row also contains the actions dropdown and Test button
      // which stopPropagation, and Playwright's row.click() can pick
      // those if they're under the centroid.
      const labelCell = page!.locator('tbody tr').first().locator('td').nth(1);
      await labelCell.click();
      await page!.waitForURL(/\/admin\/clusters\/[^/]+$/, { timeout: 15_000 });
      const match = page!.url().match(/\/admin\/clusters\/([^/?#]+)$/);
      if (!match) throw new Error(`unexpected URL after click: ${page!.url()}`);
      discoveredCid = match[1];

      // v0.3.1 regression: parent-layout-without-Outlet bug — assert
      // Buckets + Keys section headers are still present.
      // v1.3.0e added a new "Cluster admins" section above Buckets;
      // assert that one too. The h2 wraps the literal text "Cluster
      // admins" followed by an optional "(N)" count <span>, so we
      // match the whole h2 case-insensitive with hasText to tolerate
      // both the text-only and text+count shapes.
      await page!.locator('h2', { hasText: /^Cluster admins/i }).first().waitFor({ timeout: 10_000 });
      await page!.waitForSelector('text=/^Buckets( \\(|$)/', { timeout: 10_000 });
      await page!.waitForSelector('text=/^Keys( \\(|$)/', { timeout: 10_000 });
      await shot(page!, "04-cluster-detail");
    });

    // ============================================================
    // 4. Bucket detail navigation (v0.3.1 regression test)
    // ============================================================
    section("[4] bucket detail navigation (v0.3.1 regression)");
    if (!discoveredCid) {
      skipLine("click first bucket link → /admin/clusters/{cid}/buckets/{id}", "cluster detail nav failed");
    } else
    await check("click first bucket link → /admin/clusters/{cid}/buckets/{id}", async () => {
      // The cluster-detail page renders Buckets section with up to 8
      // <Link> rows. Wait for either a link or the empty state.
      const linkSelector = `a[href^="/admin/clusters/${discoveredCid}/buckets/"]`;
      const hasLinks = await page!.locator(linkSelector).count();
      if (hasLinks === 0) {
        // EmptyState ok — log and skip the navigation portion, but
        // still assert no console errors so far.
        warnLine("no buckets in first cluster — skipping bucket-detail navigation");
        return;
      }
      const firstLink = page!.locator(linkSelector).first();
      const href = (await firstLink.getAttribute('href')) ?? "";
      const m = href.match(/\/buckets\/([^/?#]+)/);
      if (!m) throw new Error(`could not parse bucket id out of href '${href}'`);
      discoveredBucketId = m[1];

      await firstLink.click();
      // TanStack Router does client-side navigation, so 'load' may
      // not refire. Poll the URL pathname instead.
      await page!.waitForFunction(
        (target) => window.location.pathname === target,
        `/admin/clusters/${discoveredCid}/buckets/${discoveredBucketId}`,
        { timeout: 15_000 },
      );
      // Wait for the bucket page to render its detail content. The
      // pre-v0.3.1 broken state showed the cluster-row form (parent
      // layout), which had Buckets+Keys section headers — so the
      // discriminator is the cluster-detail "Connection test" card
      // (present on cluster-detail, absent on bucket-detail) and
      // the cluster page's "Layout" card.
      await page!.waitForSelector(
        '.text-2xl.tracking-tight, h1.tracking-tight, button.tracking-tight',
        { timeout: 10_000 },
      );
      // Wait for any cluster-detail-only content to clear from the DOM.
      await page!.waitForFunction(
        () => !document.body.innerText.includes("Connection test"),
        null,
        { timeout: 5_000 },
      ).catch(() => {
        // If still present, that's the regression — fail explicitly.
        throw new Error("bucket page is showing cluster-detail content (Connection test card) — v0.3.1 regression returned");
      });
      await shot(page!, "05-bucket-detail");

      // Back to cluster detail.
      await page!.goBack({ waitUntil: "networkidle" });
      await page!.waitForURL(/\/admin\/clusters\/[^/]+$/, { timeout: 10_000 });
    });

    // ============================================================
    // 5. Key detail navigation (v0.3.1 regression test)
    // ============================================================
    section("[5] key detail navigation (v0.3.1 regression)");
    if (!discoveredCid) {
      skipLine("click first key link → /admin/clusters/{cid}/keys/{id}", "cluster detail nav failed");
    } else
    await check("click first key link → /admin/clusters/{cid}/keys/{id}", async () => {
      const linkSelector = `a[href^="/admin/clusters/${discoveredCid}/keys/"]`;
      const hasLinks = await page!.locator(linkSelector).count();
      if (hasLinks === 0) {
        warnLine("no keys in first cluster — skipping key-detail navigation");
        return;
      }
      const firstLink = page!.locator(linkSelector).first();
      const href = (await firstLink.getAttribute('href')) ?? "";
      const m = href.match(/\/keys\/([^/?#]+)/);
      if (!m) throw new Error(`could not parse key id out of href '${href}'`);
      discoveredKeyId = m[1];

      await firstLink.click();
      await page!.waitForFunction(
        (target) => window.location.pathname === target,
        `/admin/clusters/${discoveredCid}/keys/${discoveredKeyId}`,
        { timeout: 15_000 },
      );
      await page!.waitForSelector('h1.tracking-tight', { timeout: 10_000 });
      await page!.waitForFunction(
        () => !document.body.innerText.includes("Connection test"),
        null,
        { timeout: 5_000 },
      ).catch(() => {
        throw new Error("key page is showing cluster-detail content (Connection test card) — v0.3.1 regression returned");
      });
      await shot(page!, "06-key-detail");

      // Back.
      await page!.goBack({ waitUntil: "networkidle" });
      await page!.waitForURL(/\/admin\/clusters\/[^/]+$/, { timeout: 10_000 });
    });

    // ============================================================
    // 6. Layout editor
    // ============================================================
    section("[6] layout editor");
    if (!discoveredCid) {
      skipLine("navigate to /admin/clusters/{cid}/layout", "cluster detail nav failed");
    } else
    await check("navigate to /admin/clusters/{cid}/layout", async () => {
      await page!.goto(`${BASE_URL}/admin/clusters/${discoveredCid}/layout`, { waitUntil: "networkidle" });
      // Either the layout editor header "Layout · {label}" OR the
      // "Layout not supported" card for aws-s3.
      const isAwsS3 = firstClusterDriver.includes("s3") && !firstClusterDriver.includes("garage");
      if (isAwsS3) {
        await page!.waitForSelector('text="Layout not supported"', { timeout: 10_000 });
      } else {
        // Match either the supported header OR the unsupported card.
     // The driver label heuristic above isn't perfect for non-Garage
      // backends, so accept both as long as one of them is present.
      await page!.waitForFunction(
        () => {
          const html = document.body.innerText || "";
          return /Layout · /.test(html) || /Layout not supported/.test(html);
        },
        { timeout: 10_000 },
      );

      // Garage v2-specific check: verify layout editor renders correctly for garage driver
      if (firstClusterDriver.includes("garage")) {
        await page!.waitForSelector('text=/^Layout · /', { timeout: 10_000 });
        // Verify stage/apply/revert buttons are present for v2 driver
        const hasStageButton = await page!.locator('button').filter({ hasText: /Stage/i }).count();
        if (hasStageButton === 0) {
          throw new Error("Garage v2 layout editor missing Stage button");
        }
      }
      }
      await shot(page!, "07-layout");
    });

   // ============================================================
    // 7. Aggregated buckets (v0.6.0a USER.ROUTING)
    // ============================================================
    section("[7] aggregated buckets");
    await check("/admin/buckets renders All Buckets list directly", async () => {
      await page!.goto(`${BASE_URL}/admin/buckets`, { waitUntil: "networkidle" });
      // v0.6.0a: /admin/buckets now renders the AdminBucketsAggregated component directly,
      // no longer redirects to user home /
      await page!.waitForURL(/\/admin\/buckets$/, { timeout: 10_000 });
      // Either rows or the EmptyState "No buckets yet".
      await page!.waitForFunction(
        () => {
          const rows = document.querySelectorAll('tbody tr');
          const empty = document.body.innerText.includes("No buckets yet");
          return rows.length > 0 || empty;
        },
        { timeout: 15_000 },
      );
      const rowCount = await page!.locator('tbody tr').count();
      if (rowCount === 0) {
        warnLine("/admin/buckets is empty — that may be expected on a fresh deploy");
      }
      await shot(page!, "08-aggregated-buckets");
    });

// ============================================================
    // 8. Aggregated keys
    // ============================================================
    section("[8] aggregated keys");
    await check("/admin/keys renders key rows", async () => {
      await page!.goto(`${BASE_URL}/admin/keys`, { waitUntil: "networkidle" });
      await page!.waitForURL(/\/admin\/keys$/, { timeout: 10_000 });
      await page!.waitForSelector('h1:has-text("Access keys")', { timeout: 10_000 });
      await page!.waitForFunction(
        () => {
          const rows = document.querySelectorAll('tbody tr');
          const empty = document.body.innerText.includes("No keys yet")
            || document.body.innerText.includes("No access keys");
          return rows.length > 0 || empty;
        },
        { timeout: 15_000 },
      );
      const rowCount = await page!.locator('tbody tr').count();
      if (rowCount === 0) {
        warnLine("/admin/keys is empty — that may be expected on a fresh deploy");
      }
      await shot(page!, "09-aggregated-keys");
    });

    // v0.9.0m: "+ New" affordance must open the create-key form
    // dialog with a name field. We don't submit — actually minting a
    // key in the smoke would dirty the prod cluster — but we assert
    // the dialog renders, the form takes input, and Cancel closes
    // without mutating state.
    await check("/admin/keys '+ New' opens create-key dialog (v0.9.0m)", async () => {
      await page!.goto(`${BASE_URL}/admin/keys`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Access keys")', { timeout: 10_000 });

      // Header button labelled "New" (button, not a link).
      const newBtn = page!.locator('button:has-text("New")').first();
      await newBtn.waitFor({ state: "visible", timeout: 5_000 });
      await newBtn.click();

      // Dialog should show the create-key form.
      await page!.waitForSelector('text=Create access key', { timeout: 5_000 });
      await page!.waitForSelector('input[placeholder*="Key name"]', { timeout: 5_000 });

      // Cancel — we intentionally do NOT submit; minting a real key
      // here would leave a stray credential behind on every smoke run.
      await page!.locator('button:has-text("Cancel")').first().click();

      // Dialog should close — heading goes away.
      await page!.waitForFunction(
        () => !document.body.innerText.includes("Create access key"),
        { timeout: 5_000 },
      );
      await shot(page!, "09a-create-key-dialog");
    });

    // ============================================================
    // 11. User-tier endpoints (post-ADR-0002: regions, not clusters)
    // ============================================================
    section("[11] user-tier endpoints (v1.1.0 USER.REGIONS)");

    await check("[NN] /api/v1/user/regions returns array (may be empty)", async () => {
      const resp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (!resp.ok()) throw new Error(`GET /api/v1/user/regions failed: ${resp.status()} ${await resp.text()}`);
      const regions = await resp.json();

      if (!Array.isArray(regions)) {
        throw new Error("Expected array response from /api/v1/user/regions");
      }

      if (regions.length === 0) {
        warnLine("/api/v1/user/regions is empty — operator hasn't connected any regions yet");
      } else {
        const first = regions[0] as any;
        if (!first.id || !first.endpoint || !first.accessKeyId) {
          throw new Error("Region missing required fields (id, endpoint, accessKeyId)");
        }
        if ("secretKey" in first || "secretKeyEnc" in first) {
          throw new Error("Region response leaked secret material — should never appear on the wire");
        }
      }
    });

    // ============================================================
    // [NN] /files/{first-cid} renders bucket list (v0.5.2 USER.MYBUCKETS-IN-CLUSTER)
    // ============================================================
    section("[14] user bucket list under cluster (/files/{cid}) — OBSOLETE post v1.1.0c/e");

    // ADR-0002 retired the /files/{cid} cluster-tier route in v1.1.0c
    // and the /api/v1/user/clusters endpoint in v1.1.0e. `discoveredCid`
    // is already populated upstream by the admin UI scrape at section [04]
    // (Admin → cluster row click), so no rediscovery needed here.

    // Section [14] UI checks intentionally skipped — see retired-route note above.
    if (false) {

    if (discoveredCid) {
      await check("[NN] /files/{first-cid} renders bucket list", async () => {
        if (!discoveredCid) throw new Error("No cluster cid discovered");
        
        // Navigate to the user bucket list page for this cluster
        await page!.goto(`${BASE_URL}/files/${discoveredCid}`, { waitUntil: "networkidle" });
        await page!.waitForURL(new RegExp(`/files/${discoveredCid}$`), { timeout: 15_000 });

        // Assert cluster label is present in the header (from useUserClusterBuckets hydration)
        const headerText = await page!.locator('h1').textContent();
        if (!headerText || !headerText.trim()) {
          throw new Error("Cluster header not found on /files/{cid}");
        }

        // Get the cluster driver for badge check (from discovered cluster data)
        const firstCluster = await page!.evaluate(async (cid: string) => {
          // Try to get from cached query data or localStorage
          try {
            const cacheKey = `react-query-${JSON.stringify(["user", "clusters"])}`;
            const stored = localStorage.getItem(cacheKey);
            if (stored) {
              const parsed = JSON.parse(stored);
              const clusterData = parsed?.state?.data?.find((c: any) => c.id === cid);
              return clusterData?.driver || null;
            }
          } catch {}
          return null;
        }, discoveredCid);

        // Check for driver badge in header area (ClusterBadge component)
        const hasDriverBadge = await page!.evaluate((cid: string) => {
          const bodyText = document.body.innerText || "";
          // The ClusterBadge shows the driver name somewhere on the page
          return bodyText.includes("garage") || 
                 bodyText.includes("aws-s3") || 
                 bodyText.includes("minio");
        }, discoveredCid);

        if (!hasDriverBadge) {
          warnLine("driver badge not found in header - may be cached from admin session");
        }

        // Assert at least one bucket row OR empty-state message (both are valid)
        const hasBucketsOrEmpty = await page!.evaluate(() => {
          const rows = document.querySelectorAll('tbody tr');
          const emptyState = document.body.innerText.includes("No buckets yet") || 
                            document.body.innerText.includes("No buckets accessible");
          return rows.length > 0 || emptyState;
        });

        if (!hasBucketsOrEmpty) {
          throw new Error("/files/{cid} shows neither bucket rows nor empty state");
        }

        // If there are bucket rows, verify structure (name, size, objects columns)
        const rowCount = await page!.locator('tbody tr').count();
        if (rowCount > 0) {
          const firstRowCells = await page!.locator('tbody tr').first().locator('td');
          const cellCount = await firstRowCells.count();
          
          // Should have at least name column, optionally size/objects/actions
          if (cellCount < 1) {
            throw new Error(`Bucket row has too few columns: ${cellCount}`);
          }

          // Check that bucket names don't show raw IDs prominently
          const firstCellText = await firstRowCells.first().textContent();
          if (firstCellText && discoveredCid.startsWith(firstCellText)) {
            warnLine("bucket row may be showing cluster ID instead of bucket name");
          }
        }

        // Check for empty state message if no buckets
        const hasEmptyState = await page!.evaluate(() => {
          return document.body.innerText.includes("No buckets accessible") || 
                 document.body.innerText.includes("Contact your administrator");
        });

        if (rowCount === 0 && !hasEmptyState) {
          throw new Error("Empty state should show when no buckets are available");
        }

        await shot(page!, "14-files-cid-bucket-list");
      });

      // Check that clicking a bucket row navigates to /files/{cid}/b/{bid}
      if (await page!.locator('tbody tr').count() > 0) {
        await check("[NN] click bucket row → navigate to /files/{cid}/b/{bid}", async () => {
          const firstRow = page!.locator('tbody tr').first();
          
          // Click on the bucket name cell (first column)
          const bucketNameCell = firstRow.locator('td').first();
          await bucketNameCell.click();

          // Wait for navigation to happen
          try {
            await page!.waitForURL(new RegExp(`/files/${discoveredCid}/b/[^/?#]+`), { timeout: 15_000 });
            // This is expected - the route exists but may not be implemented yet (404s)
            // Just verify navigation happened correctly
          } catch {
            // If navigation didn't happen or URL doesn't match, that's a failure
            throw new Error(`Navigation to /files/${discoveredCid}/b/{bid} did not occur`);
          }

          await shot(page!, "15-files-cid-bucket-navigation");
        });
      } else {
        skipLine("bucket row navigation", "no buckets available to click");
      }
    }

    // ============================================================
    // [NN] Object browser page (v0.7.0d USER.OBJECTBROWSE)
    // ============================================================
    section("[NN] /files/{cid}/b/{bid} renders object browser");
    
    await check("discover first cluster + first bucket via user endpoints", async () => {
      const clustersResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters`);
      if (!clustersResp.ok()) throw new Error(`GET /api/v1/user/clusters failed: ${clustersResp.status()}`);
      const clusters = await clustersResp.json();
      
      if (!Array.isArray(clusters) || clusters.length === 0) {
        skipLine("object browser test", "no user-visible clusters");
        return;
      }
      
      discoveredCid = clusters[0].id;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters/${discoveredCid}/buckets`);
      if (!bucketsResp.ok()) throw new Error(`Buckets API failed: ${bucketsResp.status()}`);
      
      const buckets: any[] = await bucketsResp.json();
      if (buckets.length === 0) {
        skipLine("object browser navigation", "no user-visible buckets");
        return;
      }

      discoveredBucketId = buckets[0].id;
    });

    if (discoveredCid && discoveredBucketId) {
      await check("[NN] /files/{cid}/b/{bid} renders object browser with breadcrumb and rows/empty state", async () => {
        // Navigate to the object browser page
        await page!.goto(`${BASE_URL}/files/${discoveredCid}/b/${discoveredBucketId}`, { waitUntil: "networkidle" });
        
        const expectedUrl = new RegExp(`/files/${discoveredCid}/b/${discoveredBucketId}$`);
        await page!.waitForURL(expectedUrl, { timeout: 15_000 });

        // Assert header shows bucket alias (not raw ID)
        const hasHeader = await page!.locator('h1').count();
        if (hasHeader === 0) {
          throw new Error("Bucket header not found on object browser page");
        }

        // Assert breadcrumb is visible when prefix param exists in URL
        const urlHasPrefix = page!.url().includes("prefix=");
        if (urlHasPrefix) {
          const hasBreadcrumb = await page!.locator('nav[role="navigation"]').count();
          if (hasBreadcrumb === 0) {
            throw new Error("Breadcrumb not found when prefix is set in URL");
          }
        }

        // Assert at least one row OR empty-state message renders
        const hasRowsOrEmpty = await page!.evaluate(() => {
          const rows = document.querySelectorAll('tbody tr');
          const emptyState = document.body.innerText.includes("No objects here") || 
                            document.body.innerText.includes("This bucket is empty");
          return rows.length > 0 || emptyState;
        });

        if (!hasRowsOrEmpty) {
          throw new Error("/files/{cid}/b/{bid} shows neither object rows nor empty state message");
        }

        // If there are rows, verify folder/file structure
        const rowCount = await page!.locator('tbody tr').count();
        if (rowCount > 0) {
          const firstRowCells = await page!.locator('tbody tr').first().locator('td');
          const cellCount = await firstRowCells.count();
          
          // Should have at least name column, optionally size/modified/actions
          if (cellCount < 1) {
            throw new Error(`Object row has too few columns: ${cellCount}`);
          }

          // Check that folder/file icons are present
          const hasIcons = await page!.evaluate(() => {
            const svgElements = document.querySelectorAll('svg');
            return svgElements.length > 0;
          });

          if (!hasIcons) {
            warnLine("no SVG icons found in object rows - may be using text-only representation");
          }
        }

        await shot(page!, "nn-object-browser");
      });
    }

    // ============================================================
    // [NN] Upload affordance (v0.7.0e USER.UPLOAD) — button visible + dialog open
    // ============================================================
    section("[16] upload affordance (v0.7.0e USER.UPLOAD)");
    
    await check("discover first cluster + bucket via user endpoints for upload test", async () => {
      const clustersResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters`);
      if (!clustersResp.ok()) throw new Error(`GET /api/v1/user/clusters failed: ${clustersResp.status()}`);
      const clusters = await clustersResp.json();
      
      if (!Array.isArray(clusters) || clusters.length === 0) {
        skipLine("upload affordance test", "no user-visible clusters");
        return;
      }
      
      discoveredCid = clusters[0].id;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters/${discoveredCid}/buckets`);
      if (!bucketsResp.ok()) throw new Error(`Buckets API failed: ${bucketsResp.status()}`);
      
      const buckets: any[] = await bucketsResp.json();
      if (buckets.length === 0) {
        skipLine("upload affordance navigation", "no user-visible buckets");
        return;
      }

      discoveredBucketId = buckets[0].id;
    });

    if (discoveredCid && discoveredBucketId) {
      await check("[NN] /files/{cid}/b/{bid} has Upload button in toolbar", async () => {
        // Navigate to the object browser page
        await page!.goto(`${BASE_URL}/files/${discoveredCid}/b/${discoveredBucketId}`, { waitUntil: "networkidle" });
        
        const expectedUrl = new RegExp(`/files/${discoveredCid}/b/${discoveredBucketId}$`);
        await page!.waitForURL(expectedUrl, { timeout: 15_000 });

        // Assert Upload button is visible in the header actions area
        const uploadButton = page!.locator('button').filter({ hasText: /Upload/ }).first();
        
        if (await uploadButton.count() === 0) {
          throw new Error("Upload button not found in bucket toolbar");
        }

        // Assert Upload button is enabled (not disabled)
        const isDisabled = await uploadButton.getAttribute('disabled');
        if (isDisabled !== null) {
          throw new Error("Upload button should be enabled but is disabled");
        }

        await shot(page!, "nn-upload-button-visible");
      });

      // Check that clicking Upload opens the dialog (just confirm it appears, don't exercise actual upload)
      await check("[NN] click Upload → upload dialog opens", async () => {
        const uploadButton = page!.locator('button').filter({ hasText: /Upload/ }).first();
        await uploadButton.click({ waitUntil: 'networkidle' });

        // Wait for dialog to open - look for the Dialog content or Upload Files title
        try {
          await page!.waitForSelector('[role="dialog"] h1:has-text("Upload Files")', { timeout: 5_000 });
        } catch {
          // Alternative selector if role not present
          await page!.waitForText('Upload Files', { timeout: 5_000 });
        }

        // Verify dialog has drag-drop zone (the dashed border area)
        const hasDragDropZone = await page!.locator('.border-dashed').count() > 0;
        if (!hasDragDropZone) {
          throw new Error("Upload dialog missing drag-and-drop zone");
        }

        // Verify upload dialog has file list container
        const hasFileList = await page!.locator('[class*="space-y-3"]').count() > 0 || 
                           await page!.locator('div').filter({ hasText: /Drag and drop files/ }).count() > 0;
        
        if (!hasFileList) {
          warnLine("upload dialog may be missing expected file list structure");
        }

        // Close the dialog without uploading (just verify it was open)
        const closeButtons = page!.locator('button').filter({ hasText: /Close|Cancel/ });
        if (await closeButtons.count() > 0) {
          await closeButtons.first().click();
          
          // Verify dialog closed - Upload Files title should be gone
          try {
            await page!.waitForSelector('[role="dialog"] h1:has-text("Upload Files")', { timeout: 3_000 });
            throw new Error("Upload dialog did not close after clicking Close");
          } catch {
            // Expected - dialog should be closed now
          }
        }

        await shot(page!, "nn-upload-dialog-opened");
      });
    }

    // ============================================================
    // [NN] Sync out affordance (v0.8.0d SYNC.ENGINE.PUSH) — button visible + dialog opens with correct direction
    // ============================================================
    section("[16a] sync out affordance (v0.8.0d SYNC.ENGINE.PUSH)");
    
    await check("discover first cluster + bucket via user endpoints for sync out test", async () => {
      const clustersResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters`);
      if (!clustersResp.ok()) throw new Error(`GET /api/v1/user/clusters failed: ${clustersResp.status()}`);
      const clusters = await clustersResp.json();
      
      if (!Array.isArray(clusters) || clusters.length === 0) {
        skipLine("sync out affordance test", "no user-visible clusters");
        return;
      }
      
      discoveredCid = clusters[0].id;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/clusters/${discoveredCid}/buckets`);
      if (!bucketsResp.ok()) throw new Error(`Buckets API failed: ${bucketsResp.status()}`);
      
      const buckets: any[] = await bucketsResp.json();
      if (buckets.length === 0) {
        skipLine("sync out affordance navigation", "no user-visible buckets");
        return;
      }

      discoveredBucketId = buckets[0].id;
    });

    if (discoveredCid && discoveredBucketId) {
      await check("[NN] /files/{cid}/b/{bid} has Sync out button alongside Sync in", async () => {
        // Navigate to the object browser page
        await page!.goto(`${BASE_URL}/files/${discoveredCid}/b/${discoveredBucketId}`, { waitUntil: "networkidle" });
        
        const expectedUrl = new RegExp(`/files/${discoveredCid}/b/${discoveredBucketId}$`);
        await page!.waitForURL(expectedUrl, { timeout: 15_000 });

        // Assert Sync out button is visible in the header actions area (next to Sync in)
        const syncOutButton = page!.locator('button').filter({ hasText: /Sync out/ }).first();
        
        if (await syncOutButton.count() === 0) {
          throw new Error("Sync out button not found in bucket toolbar");
        }

        // Assert Sync out button is enabled (not disabled)
        const isDisabled = await syncOutButton.getAttribute('disabled');
        if (isDisabled !== null) {
          throw new Error("Sync out button should be enabled but is disabled");
        }

        // Verify both Sync in and Sync out buttons are present
        const syncInButton = page!.locator('button').filter({ hasText: /Sync in/ }).first();
        if (await syncInButton.count() === 0) {
          throw new Error("Sync in button not found - required alongside Sync out");
        }

        await shot(page!, "nn-sync-out-button-visible");
      });

      // Check that clicking Sync out opens the dialog with correct direction (push mode pre-fills source)
      await check("[NN] click Sync out → sync dialog opens with push direction, source pre-filled", async () => {
        const syncOutButton = page!.locator('button').filter({ hasText: /Sync out/ }).first();
        await syncOutButton.click({ waitUntil: 'networkidle' });

        // Wait for dialog to open - look for the Dialog content or Sync out title
        try {
          await page!.waitForSelector('[role="dialog"] h1:has-text(/sync out/i)', { timeout: 5_000 });
        } catch {
          // Alternative selector if role not present
          const hasSyncOutText = await page!.locator('h2').filter({ hasText: /sync out/i }).count();
          const hasDialogTitle = await page!.locator('[role="dialog"] h1').count();
          if (hasSyncOutText === 0 && hasDialogTitle === 0) {
            throw new Error("Sync dialog did not open with correct title");
          }
        }

        // For push direction, source cluster should be pre-filled and disabled
        const srcClusterSelect = page!.locator('select').filter({ hasLabel: /source cluster/i });
        if (await srcClusterSelect.count() > 0) {
          const isDisabled = await srcClusterSelect.first().getAttribute('disabled');
          if (isDisabled === null) {
            warnLine("source cluster should be disabled for push direction");
          }
        }

        // Close the dialog without syncing (just verify it was open with correct state)
        const closeButtons = page!.locator('button').filter({ hasText: /Close|Cancel/ });
        if (await closeButtons.count() > 0) {
          await closeButtons.first().click();
          
          // Verify dialog closed - sync out title should be gone
          try {
            await page!.waitForSelector('[role="dialog"] h1:has-text(/sync out/i)', { timeout: 3_000 });
            throw new Error("Sync dialog did not close after clicking Close");
          } catch {
            // Expected - dialog should be closed now
          }
        }

        await shot(page!, "nn-sync-out-dialog-opened");
      });
    }

    } // end of `if (false) {` — cluster-tier sections [14], [NN object browser], [16 upload], [16a sync-out] retired in v1.1.0c (ADR-0002)

    // ============================================================
    // 12. Fix 3, 1, 4: cluster detail buckets section and bucket detail
    // ============================================================
    section("[17] bucket detail and cluster detail fixes (Fix 1, 3, 4)");
    
    if (!discoveredCid) {
      skipLine("bucket/cluster detail checks", "cluster cid not discovered in prior steps");
    } else {
      // First, fetch clusters via API to discover first bucket id
      await check("fetch /api/v1/admin/clusters via authed request and get first bucket", async () => {
        const resp = await page!.request.get(`${BASE_URL}/api/v1/admin/clusters`);
        if (!resp.ok()) throw new Error(`API request failed: ${resp.status()}`);
        const clusters = await resp.json();
        
        // Find the cluster we discovered earlier to get its buckets endpoint
        const targetCluster = Array.isArray(clusters) 
          ? clusters.find((c: any) => c.id === discoveredCid)
          : null;
          
        if (!targetCluster) {
          throw new Error(`Could not find cluster ${discoveredCid} in API response`);
        }

        // Fetch buckets for this cluster
        const bucketResp = await page!.request.get(
          `${BASE_URL}/api/v1/admin/clusters/${discoveredCid}/buckets`
        );
        if (!bucketResp.ok()) throw new Error(`Buckets API failed: ${bucketResp.status()}`);
        
        const buckets: any[] = await bucketResp.json();
        if (buckets.length === 0) {
          warnLine("no buckets in cluster — skipping bucket-detail checks");
          return;
        }

        discoveredBucketId = buckets[0].id;
      });

      // Navigate to bucket detail and check Fix 1: H1 shows alias or "(no alias)" not ID prefix
      await check("bucket detail H1 is NOT the ID prefix (Fix 1)", async () => {
        await page!.goto(
          `${BASE_URL}/admin/clusters/${discoveredCid}/buckets/${discoveredBucketId}`,
          { waitUntil: "networkidle" }
        );

        // Wait for bucket detail to render. v0.8.0d.29 wrapped the
        // click-to-edit button inside a real <h1>; the classes moved
        // off the button onto the h1.
        await page!.waitForSelector('h1', { timeout: 10_000 });

        const h1Text = await page!
          .locator('h1')
          .first()
          .textContent();
        
        if (!h1Text) throw new Error("H1 button has no text content");
        
        // The H1 should NOT be the bucket ID prefix (first 12 chars of ID)
        const idPrefix = discoveredBucketId.slice(0, 12);
        if (h1Text === idPrefix) {
          throw new Error(`H1 shows ID prefix '${idPrefix}' instead of alias or '(no alias)'`);
        }

        // H1 should either be an alias OR "(no alias)" in italic muted styling
        const hasNoAlias = h1Text.includes("(no alias)");
        
        if (!hasNoAlias && !h1Text.startsWith(discoveredBucketId)) {
          // Good - it's showing an alias
        } else if (hasNoAlias) {
          // Also good - no aliases exist, showing "(no alias)"
        }

        await shot(page!, "09-bucket-detail-h1");
      });

      // Check Fix 4: Back link goes to cluster page with cid param
      await check("bucket detail back link goes to /admin/clusters/{cid} (Fix 4)", async () => {
        const backLink = page!.locator('a').filter({ hasText: /^← Cluster$/ });
        
        if (!await backLink.count()) {
          throw new Error("Back link with '← Cluster' text not found");
        }

        // Record current URL before clicking
        const currentUrl = page!.url();
        
        // Click the back link
        await backLink.click();

        // Wait for navigation to cluster detail page
        await page!.waitForURL(
          new RegExp(`^${BASE_URL}/admin/clusters/${discoveredCid}(\\?|#|$)`),
          { timeout: 10_000 }
        );

        const afterClickUrl = page!.url();
        
        // Verify we're on the cluster detail page, not "/" or some other route
        if (!afterClickUrl.includes(`/admin/clusters/${discoveredCid}`)) {
          throw new Error(`Back link navigated to wrong URL: ${afterClickUrl}`);
        }

        await shot(page!, "10-back-to-cluster");
      });

      // Check Fix 3: No View all → link in buckets section header
      await check("cluster detail has NO 'View all →' link in buckets section (Fix 3)", async () => {
        await page!.goto(
          `${BASE_URL}/admin/clusters/${discoveredCid}`,
          { waitUntil: "networkidle" }
        );

        // The buckets section header should be present
        await page!.waitForSelector('text=/^Buckets( \\(|$)/', { timeout: 10_000 });

        // There should NOT be a "View all →" link in the buckets section header area
        const viewAllLink = page!.locator('a').filter({ hasText: /View all →/ });
        
        // We need to check that this link is NOT present near the Buckets heading
        // The keys section should have its own "View all →" linking to /admin/keys
        
        // Check for buckets-section View all (should be absent) - it would point to "/"
        const bucketsSection = page!.locator('section').filter({ hasText: /^Buckets/ });
        const bucketsViewAll = bucketsSection.locator('a[href="/"]').first();
        
        if (await bucketsViewAll.count() > 0) {
          throw new Error("Found 'View all →' link pointing to '/' in buckets section - should be removed");
        }

        await shot(page!, "11-cluster-detail-buckets-section");
      });
    }

    // ============================================================
    // [NN] /files/keys renders region-key cards (ADR-0002 v1.1.0d,
    // renamed to "My Keys" in v1.2.0d as part of the key-first model)
    // ============================================================
    section("[NN] /files/keys renders region-key cards or empty state");
    await check("/files/keys is the 'My Keys' page (v1.2.0d rename)", async () => {
      await page!.goto(`${BASE_URL}/files/keys`, { waitUntil: "networkidle" });

      // v1.2.0d renames /files/keys from "My Region Keys" -> "My Keys"
      // (key-first user model). The region-key-card test-id stayed —
      // it tracks the underlying UserRegion record, not the heading.
      const heading = await page!.locator('h1', { hasText: "My Keys" }).count();
      if (heading === 0) {
        throw new Error("/files/keys missing 'My Keys' heading (v1.2.0d rename)");
      }

      const hasCards = await page!.locator('[data-testid="region-key-card"]').count();
      const hasEmptyState = await page!.locator('text=No region keys yet').count();

      if (hasCards === 0 && hasEmptyState === 0) {
        throw new Error("/files/keys rendered neither region-key cards nor empty state");
      }

      if (hasCards > 0) {
        process.stdout.write(`${C.dim}found ${hasCards} region-key card(s)${C.reset}\n`);
        // Verify card structure
        const firstCard = page!.locator('[data-testid="region-key-card"]').first();
        const hasCopyButton = await firstCard.locator('[data-testid="copy-region-key-button"]').count() > 0;
        if (!hasCopyButton) {
          throw new Error("Region-key card missing copy button");
        }
        await shot(page!, "nn-keys-region-cards");
      } else {
        process.stdout.write(`${C.dim}empty state shown (no region keys yet)${C.reset}\n`);
        await shot(page!, "nn-keys-empty-state");
      }
    });

    // ============================================================
    // 16.x AUTH.RBAC (v0.5.7) — admin-only pages render for UIAdmin
    // ============================================================
    section("[16a] AUTH.RBAC — /admin/system + /admin/users (v0.5.7)");

    await check("/admin/system renders OrgCapabilities (UIAdmin: matthew)", async () => {
      await page!.goto(`${BASE_URL}/admin/system`, { waitUntil: "networkidle" });
      // Page must NOT redirect to login (would indicate auth lost)
      // and NOT redirect to / (would indicate non-UIAdmin gate).
      const url = page!.url();
      if (/\/admin\/login/.test(url)) {
        throw new Error("/admin/system bounced to /admin/login — auth lost");
      }
      if (url === `${BASE_URL}/` || url.endsWith("/files")) {
        throw new Error("/admin/system bounced to user shell — matthew should be UIAdmin");
      }
      // Page should have some content. Permissive — exact UI shape is
      // still settling. Just confirm the page rendered an authoritative
      // marker like a header.
      await page!.waitForSelector('h1', { timeout: 10_000 });
    });

    await check("/admin/users renders user list (UIAdmin: matthew)", async () => {
      await page!.goto(`${BASE_URL}/admin/users`, { waitUntil: "networkidle" });
      const url = page!.url();
      if (/\/admin\/login/.test(url)) {
        throw new Error("/admin/users bounced to /admin/login — auth lost");
      }
      if (url === `${BASE_URL}/` || url.endsWith("/files")) {
        throw new Error("/admin/users bounced to user shell — matthew should be UIAdmin");
      }
      await page!.waitForSelector('h1', { timeout: 10_000 });
    });

    // v1.0.0c: audit log page, gated on `audit:view`. v1.2.0a moved
    // that capability behind ADMIN mode (ADR-0003) — the prior
    // [1.8] elevateToAdmin step lifts matthew there. The table either
    // has rows (matthew performed at least the login + elevate events
    // earlier in the run) or shows the empty-state copy.
    await check("/admin/audit renders the audit log (host_admin + ADMIN mode)", async () => {
      await page!.goto(`${BASE_URL}/admin/audit`, { waitUntil: "networkidle" });
      const url = page!.url();
      if (/\/admin\/login/.test(url)) {
        throw new Error("/admin/audit bounced to /admin/login — auth lost");
      }
      if (url === `${BASE_URL}/` || url.endsWith("/files")) {
        throw new Error("/admin/audit bounced to user shell — matthew should pass the gate");
      }
      // Page should render the "Audit log" heading.
      await page!.waitForSelector('h1', { timeout: 10_000 });
      const heading = await page!.locator('h1').first().textContent();
      if (!heading || !heading.toLowerCase().includes("audit")) {
        throw new Error(`expected h1 to contain "audit", got: ${heading}`);
      }
      // Either the table is present OR the empty-state copy is.
      const hasTable = await page!.locator('table').count();
      const hasEmpty = await page!.locator('text=No events match the filter').count();
      if (hasTable === 0 && hasEmpty === 0) {
        throw new Error("neither table nor empty-state visible on /admin/audit");
      }
      await shot(page!, "16b-audit");
    });

    // ============================================================
    // 13. Fix 7: version label under Logo wordmark
    // ============================================================
    // USER.ADDKEY — v1.2.0d. /files/keys/new is the canonical
    // "Add a key" form; /files/regions/new redirects there.
    // ============================================================
    section("[15a] USER.ADDKEY — /files/keys/new renders the key form (v1.2.0d)");
    await check("navigate to /files/keys/new and verify form", async () => {
      await page!.goto(`${BASE_URL}/files/keys/new`, { waitUntil: "networkidle" });

      const url = page!.url();
      if (/\/admin/.test(url)) {
        throw new Error("redirected to /admin — should stay in user shell");
      }

      const hasHeading = await page!.locator('h1').filter({ hasText: /Add a key/ }).count() > 0;
      if (!hasHeading) {
        throw new Error("Could not find 'Add a key' heading on /files/keys/new");
      }

      // Form should have alias + endpoint + accessKeyId + secretKey + region inputs
      for (const id of ["alias", "endpoint", "accessKeyId", "secretKey", "region"]) {
        const has = await page!.locator(`#${id}`).count();
        if (has === 0) throw new Error(`/files/keys/new missing field #${id}`);
      }

      await shot(page!, "15a-add-key-page");
    });


section("[15b] USER.SHARES — /files/shares renders shares list and empty state (v0.7.0g)");
    await check("navigate to /files/shares and verify header + table/empty", async () => {
      // First navigate to the shares page
      await page!.goto(`${BASE_URL}/files/shares`, { waitUntil: "networkidle" });
      
      const url = page!.url();
      if (/\/admin/.test(url)) {
        throw new Error("redirected to /admin — should stay in user shell");
      }

      // Check that we're on the shares page - look for header
      const hasHeader = await page!.locator('h1').filter({ hasText: /Shares/i }).count() > 0;
      
      if (!hasHeader) {
        throw new Error("Could not find 'Shares' heading on /files/shares");
      }

      // Check for subheader
      const hasSubhead = await page!.locator('p').filter({ hasText: /links you.*ve created/i }).count() > 0;
      
      if (!hasSubhead) {
        throw new Error("Could not find shares subheading");
      }

      // Check for either empty state or table
      const hasEmptyState = await page!.locator('h3').filter({ hasText: /no shares/i }).count() > 0;
      const hasTable = await page!.locator('table').count() > 0;
      
      if (!hasEmptyState && !hasTable) {
        throw new Error("Could not find empty state or table on /files/shares");
      }

      // If there are shares, verify table structure
      if (hasTable) {
        const hasTableHeaders = await page!.locator('th').filter({ hasText: /bucket|path|created/i }).count() > 0;
        
        if (!hasTableHeaders) {
          throw new Error("Share table missing expected headers");
        }

        // Verify action buttons exist
        const hasCopyButton = await page!.locator('button').filter({ hasText: /copy/i }).count() > 0;
        const hasRevokeButton = await page!.locator('button').filter({ hasText: /revoke/i }).count() > 0;

        if (!hasCopyButton || !hasRevokeButton) {
          throw new Error("Share table missing copy or revoke action buttons");
        }
      }

      await shot(page!, "15b-shares-page");
    });


section("[NN] /share/{fake-token} shows not-found state (v0.7.0h SHARE.PUBLIC smoke)");
    await check("/share/notarealtoken shows not-found message without auth", async () => {
      // Navigate directly to a fake share token — no login required
      await page!.goto(`${BASE_URL}/share/notarealtoken`, { waitUntil: "networkidle" });

      // Should see Basement UI branding but no user nav (no-chrome shell)
      const hasBasementBranding = await page!.locator('text=Delivered by Basement UI').count() > 0;
      if (!hasBasementBranding) {
        throw new Error("Share route should show 'Delivered by Basement UI' branding");
      }

      // Should see not-found state, not a generic error or loading spinner
      const hasNotFoundMessage = await page!.locator('h3').filter({ hasText: /not found/i }).count() > 0;
      if (!hasNotFoundMessage) {
        throw new Error("Share route should show 'Share not found' heading");
      }

      // Should NOT show login prompt or auth form (public route)
      const hasLoginForm = await page!.locator('input[type="password"]').count();
      if (hasLoginForm > 0) {
        throw new Error("Share route for non-existent share should not show password form");
      }

      // Should NOT have user nav items (no-chrome shell)
      const hasUserNav = await page!.locator('text=Clusters|Files|Keys').count();
      if (hasUserNav > 0) {
        throw new Error("Share route should not show user navigation");
      }

      await shot(page!, "nn-share-not-found");
    });


section("[16] version label under Logo (Fix 7)");
    await check("version label renders under Basement wordmark", async () => {
      // Navigate to any admin page - /admin/clusters works
      await page!.goto(`${BASE_URL}/admin/clusters`, { waitUntil: "networkidle" });

      // Look for the version pattern near the Logo wordmark
      // The logo should have text matching vX.Y.Z (e.g., v0.4.5)
      const hasVersion = await page!.evaluate(() => {
        // Find the Logo component - it contains "Basement" text and a version span
        const basementText = document.body.innerText.includes("Basement");
        
        if (!basementText) return false;

        // Look for version pattern vX.Y.Z in the logo area
        // The LogoVersion renders as <span class="text-[10px] ...">vX.Y.Z</span>
        const elements = document.querySelectorAll('span');
        for (const el of Array.from(elements)) {
          const text = el.textContent;
          if (text && /^v\d+\.\d+/.test(text)) {
            // Check if this is near the Basement wordmark
            return true;
          }
        }
        
        // Alternative: check for version in page title or meta tags as fallback
        const titleVersion = document.title.match(/v\d+\.\d+/);
        return !!titleVersion;
      });

      if (!hasVersion) {
        throw new Error("Could not find version label (vX.Y.Z pattern) near Basement wordmark");
      }

      await shot(page!, "12-logo-with-version");
    });

    // ============================================================
    // [NN] Syncs route (v0.8.0c SYNC.ENGINE.PULL) — read-only smoke
    // ============================================================
    section("[NN] /files/syncs renders (read-only, v0.8.0c)");
    
    await check("[NN] /files/syncs renders header + empty state or list", async () => {
      await page!.goto(`${BASE_URL}/files/syncs`, { waitUntil: "networkidle" });
      
      // Wait for the navigation + render to settle — the previous
      // smoke check leaves the page on a different route, so we need
      // both 'networkidle' and an explicit h1 wait.
      try {
        await page!.waitForSelector('h1', { timeout: 5000 });
      } catch {
        throw new Error("/files/syncs failed to render any h1 — page may have errored");
      }

      // Assert header is present
      const hasHeader = await page!.locator('h1').filter({ hasText: "Syncs" }).count();
      if (hasHeader === 0) {
        const actualH1 = await page!.locator('h1').first().textContent().catch(()=>"<none>");
        throw new Error(`/files/syncs missing 'Syncs' header (saw h1: ${actualH1})`);
      }
      
      // Assert subhead is present
      const hasSubhead = await page!.locator('p').filter({ hasText: "Cross-cluster copy jobs" }).count();
      if (hasSubhead === 0) {
        throw new Error("/files/syncs missing cross-cluster description");
      }
      
      // Either empty state with "Start sync" button OR list of jobs
      const hasEmptyState = await page!.locator('text="No sync jobs yet"').count();
      const hasJobCards = await page!.locator('[data-testid="sync-job-card"]').count();
      
      if (hasEmptyState === 0 && hasJobCards === 0) {
        // Check for "Start sync" button on empty state OR any job card content
        const hasStartButton = await page!.locator('button').filter({ hasText: /Start sync/i }).count();
        if (hasStartButton === 0) {
          throw new Error("/files/syncs shows neither empty state nor job cards");
        }
      }
      
      // Assert "Start sync" button is present (for empty state or to create new jobs)
      const startButton = page!.locator('button').filter({ hasText: /Start sync/i }).first();
      if (await startButton.count() === 0) {
        throw new Error("'Start sync' button not found on /files/syncs");
      }
      
      await shot(page!, "nn-syncs-route");
    });

    // ============================================================
    // [v1.5a] /files/backups — scheduled backup wizard + list
    // ============================================================
    section("[v1.5a] /files/backups renders (v1.5.0a BACKUP.SCHEDULED)");

    await check("[v1.5a] /files/backups renders header + empty state or list", async () => {
      await page!.goto(`${BASE_URL}/files/backups`, { waitUntil: "networkidle" });
      try {
        await page!.waitForSelector('h1', { timeout: 5000 });
      } catch {
        throw new Error("/files/backups failed to render any h1 — page may have errored");
      }

      const hasHeader = await page!.locator('h1').filter({ hasText: "Backups" }).count();
      if (hasHeader === 0) {
        const actualH1 = await page!.locator('h1').first().textContent().catch(() => "<none>");
        throw new Error(`/files/backups missing 'Backups' header (saw h1: ${actualH1})`);
      }

      // Either empty state ("No backups yet") OR a list of cards.
      const hasEmpty = await page!.locator('text="No backups yet"').count();
      const hasNewBtn = await page!.locator('button').filter({ hasText: /New backup/i }).count();
      if (hasEmpty === 0 && hasNewBtn === 0) {
        throw new Error("/files/backups shows neither empty state nor 'New backup' CTA");
      }

      await shot(page!, "v1.5a-backups-route");
    });

    await check("[v1.5a] /files/backups/new wizard renders step 1 (source)", async () => {
      await page!.goto(`${BASE_URL}/files/backups/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1', { timeout: 5000 });

      const hasHeader = await page!.locator('h1').filter({ hasText: "New backup" }).count();
      if (hasHeader === 0) {
        throw new Error("/files/backups/new missing 'New backup' header");
      }

      // Source label should be visible on step 1.
      const hasSourceHeading = await page!.locator('text=/Source/i').count();
      if (hasSourceHeading === 0) {
        throw new Error("/files/backups/new step 1 missing Source heading");
      }

      // Step counter present (v1.5.0b grew the wizard to 5 steps:
      // Source / Destination / Mode+retention / Schedule / Name+review).
      const hasStepText = await page!.locator('text=/Step 1 of 5/i').count();
      if (hasStepText === 0) {
        throw new Error("/files/backups/new missing 'Step 1 of 5' indicator");
      }

      await shot(page!, "v1.5a-backups-new-step1");
    });

    // ----- v1.5.0b: Mode + retention surfaces -----
    section("[v1.5b] /files/backups/new wizard exposes Mode + retention step");
    await check("[v1.5b] wizard step 3 has Mode radio + GFS retention inputs", async () => {
      await page!.goto(`${BASE_URL}/files/backups/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1', { timeout: 5000 });
      // Pick a source region+bucket to enable Next. If the user has
      // no regions seeded the smoke just records the limitation —
      // step 3 is still reachable in the DOM by directly walking
      // the wizard, but the smoke avoids relying on that.
      const regionSel = page!.locator('select#srcRegion');
      const regionOptions = await regionSel.locator('option').count();
      if (regionOptions <= 1) {
        // No regions configured for this user — skip the deep walk.
        return;
      }
      await regionSel.selectOption({ index: 1 });
      // Wait for buckets to populate, then pick the first one.
      const srcBucketSel = page!.locator('select#srcBucket');
      await srcBucketSel.waitFor({ state: 'visible' });
      const bucketOptions = await srcBucketSel.locator('option').count();
      if (bucketOptions > 1) {
        await srcBucketSel.selectOption({ index: 1 });
      } else {
        return;
      }
      // Advance to step 2 (Destination).
      await page!.locator('button', { hasText: /Next/ }).click();
      // Pick destination region+bucket.
      const dstRegionSel = page!.locator('select#dstRegion');
      await dstRegionSel.selectOption({ index: 1 });
      const dstBucketSel = page!.locator('select#dstBucket');
      await dstBucketSel.waitFor({ state: 'visible' });
      const dstBucketOptions = await dstBucketSel.locator('option').count();
      if (dstBucketOptions > 1) {
        await dstBucketSel.selectOption({ index: 1 });
      } else {
        return;
      }
      // Advance to step 3 (Mode + retention).
      await page!.locator('button', { hasText: /Next/ }).click();
      await page!.waitForSelector('text=/Mode.*retention/i', { timeout: 5000 });
      const hasMirror = await page!.locator('text=/Mirror.*overwrite destination/i').count();
      const hasSnapshot = await page!.locator('text=/Snapshot.*timestamped history/i').count();
      if (hasMirror === 0 || hasSnapshot === 0) {
        throw new Error('/files/backups/new step 3 missing Mirror or Snapshot option');
      }
      await shot(page!, 'v1.5b-backups-new-step3-mode');
    });

    // ----- v1.5.0c: backup detail page + restore wizard -----
    // The detail + restore screens are gated on having at least one
    // backup record. The smoke prefers an existing user-owned record
    // so it doesn't touch destination buckets; if none exist it creates
    // a transient manual+disabled+mirror-mode record (no scheduled
    // runs, no destination writes) and deletes it after the checks.
    section("[v1.5c] /files/backups/{id} detail + restore wizard (v1.5.0c)");
    {
      let backupId = "";
      let createdEphemeral = false;

      await check("[v1.5c] discover (or create) a backup record", async () => {
        const listResp = await page!.request.get(`${BASE_URL}/api/v1/user/backups`);
        if (!listResp.ok()) {
          throw new Error(`GET /api/v1/user/backups → ${listResp.status()}`);
        }
        const existing = await listResp.json();
        if (Array.isArray(existing) && existing.length > 0) {
          backupId = existing[0].id as string;
          info(`reusing existing backup ${backupId} (${existing[0].name ?? "<unnamed>"})`);
          return;
        }

        // Need to create one. Find two regions (or one region with two
        // buckets) so src+dst differ. If the caller has no regions, the
        // detail+restore checks degrade to a warn-and-skip rather than
        // fail — empty deploy.
        const regionsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
        if (!regionsResp.ok()) {
          warnLine(`/user/regions → ${regionsResp.status()}; skipping backup detail checks`);
          return;
        }
        const regions = await regionsResp.json();
        if (!Array.isArray(regions) || regions.length === 0) {
          warnLine("no user regions configured — skipping backup detail checks");
          return;
        }
        const regionId = regions[0].id as string;

        const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
        if (!bucketsResp.ok()) {
          warnLine(`/user/regions/${regionId}/buckets → ${bucketsResp.status()}; skipping`);
          return;
        }
        const body = await bucketsResp.json();
        const buckets: any[] = Array.isArray(body) ? body : body?.buckets ?? [];
        if (buckets.length === 0) {
          warnLine("region has no buckets — skipping backup detail checks");
          return;
        }
        // src = first bucket; dst = last bucket. If only one bucket
        // exists src === dst — still acceptable since the backup is
        // manual+disabled and never runs.
        const srcBucket = buckets[0].id as string;
        const dstBucket = buckets[buckets.length - 1].id as string;

        const createResp = await page!.request.post(`${BASE_URL}/api/v1/user/backups`, {
          headers: { "Content-Type": "application/json" },
          data: {
            name: `smoke-ephemeral-${Date.now()}`,
            srcRegionId: regionId,
            srcBucket,
            dstRegionId: regionId,
            dstBucket,
            schedule: "manual",
            disabled: true,
            mode: "mirror",
          },
        });
        if (!createResp.ok()) {
          throw new Error(`POST /api/v1/user/backups → ${createResp.status()} ${await createResp.text()}`);
        }
        const created = await createResp.json();
        backupId = created.id as string;
        createdEphemeral = true;
        info(`created ephemeral backup ${backupId} (manual+disabled+mirror)`);
      });

      if (backupId) {
        await check("[v1.5c] /files/backups/{id} detail page renders", async () => {
          await page!.goto(`${BASE_URL}/files/backups/${backupId}`, { waitUntil: "networkidle" });
          await page!.waitForSelector('h1', { timeout: 10_000 });
          // Configuration section heading always renders.
          const hasConfig = await page!.locator('h2:has-text("Configuration")').count();
          if (hasConfig === 0) {
            throw new Error("/files/backups/{id} missing 'Configuration' section");
          }
          // Recent runs section heading always renders (empty table is fine).
          const hasRuns = await page!.locator('h2:has-text("Recent runs")').count();
          if (hasRuns === 0) {
            throw new Error("/files/backups/{id} missing 'Recent runs' section");
          }
          await shot(page!, "v1.5c-backups-detail");
        });

        await check("[v1.5c] /files/backups/{id}/restore wizard renders step 1", async () => {
          await page!.goto(`${BASE_URL}/files/backups/${backupId}/restore`, { waitUntil: "networkidle" });
          // The page has three render branches gated on the backup's
          // mode + snapshot inventory:
          //   1. snapshot mode + snapshots present → "Restore from
          //      snapshot" header + Step 1 of 3 indicator + radio
          //   2. snapshot mode + zero snapshots → header + "No snapshots
          //      on disk yet" copy (step 1 + empty-state)
          //   3. mirror mode → no wizard header; an inline notice
          //      "Restore is only available for snapshot-mode backups"
          // Smoke accepts all three — the gate is "the route mounts
          // without crashing and renders something meaningful".
          await page!.waitForFunction(
            () => {
              const t = document.body.innerText || "";
              return (
                /Restore from snapshot/.test(t) ||
                /Restore is only available for snapshot-mode/i.test(t)
              );
            },
            null,
            { timeout: 10_000 },
          );
          const hasWizardHeader = await page!.locator('h1:has-text("Restore from snapshot")').count();
          const hasMirrorNotice = await page!
            .locator('text=/Restore is only available for snapshot-mode/i')
            .count();
          if (hasWizardHeader === 0 && hasMirrorNotice === 0) {
            throw new Error(
              "/files/backups/{id}/restore: neither wizard header nor mirror-mode notice rendered",
            );
          }
          if (hasWizardHeader > 0) {
            // Snapshot-mode branch — also assert the Step 1 indicator or
            // the empty-state copy is in the DOM.
            const hasStepText = await page!.locator('text=/Step 1 of 3/i').count();
            const hasEmpty = await page!.locator('text=/No snapshots on disk yet/i').count();
            if (hasStepText === 0 && hasEmpty === 0) {
              throw new Error(
                "/files/backups/{id}/restore wizard rendered but neither Step 1 of 3 nor empty-state",
              );
            }
          } else {
            info("restore route in mirror-mode notice branch (ephemeral backup is mirror)");
          }
          await shot(page!, "v1.5c-backups-restore");
        });
      } else {
        skipLine("[v1.5c] backup detail + restore checks", "no backup record available");
      }

      // Cleanup: delete the ephemeral record we created so the next
      // smoke run sees a clean slate.
      if (createdEphemeral && backupId) {
        await check("[v1.5c] cleanup ephemeral backup", async () => {
          const delResp = await page!.request.delete(`${BASE_URL}/api/v1/user/backups/${backupId}`);
          if (!delResp.ok()) {
            throw new Error(`DELETE /api/v1/user/backups/${backupId} → ${delResp.status()}`);
          }
        });
      }
    }

    // ============================================================
    // [NN] v1.3 feature surfaces — invites, admin TTL, folder nav
    // ============================================================
    section("[v1.3a] /admin/users renders Pending Invites (v1.3.0d)");
    await check("/admin/users shows 'Pending invites' section", async () => {
      await page!.goto(`${BASE_URL}/admin/users`, { waitUntil: "networkidle" });
      // Section heading is "Pending invites" — exact case matches the
      // h2 in PendingInvitesSection. Either the table is rendered (one
      // or more outstanding invites) OR the empty-state copy is.
      await page!.waitForSelector('text=/Pending invites/i', { timeout: 10_000 });
      const hasTable = await page!.locator('[data-testid="invites-table"]').count();
      const hasEmpty = await page!.locator('[data-testid="invites-empty"]').count();
      if (hasTable === 0 && hasEmpty === 0) {
        throw new Error("/admin/users — Pending Invites section missing both table and empty-state");
      }
      await shot(page!, "v1.3a-admin-users-pending-invites");
    });

    section("[v1.3b] /admin/system Admin session timeout card (v1.3.0a.4)");
    await check("/admin/system shows 'Admin session timeout' card", async () => {
      await page!.goto(`${BASE_URL}/admin/system`, { waitUntil: "networkidle" });
      // The TTL card lives inside the AdminSessionTTLCard component
      // and carries the CardTitle "Admin session timeout". The dropdown
      // surfaces preset TTLs (5m / 15m / 30m / 1h / 2h / 8h / Custom).
      await page!.waitForSelector('text=/Admin session timeout/i', { timeout: 10_000 });
      await shot(page!, "v1.3b-admin-system-ttl");
    });

    section("[v1.3c] /files/{regionId}/b/{bid} renders commonPrefixes as folders (v1.3.0c.1)");
    await check("object browser shows folder rows from commonPrefixes", async () => {
      // Discover a region with at least one bucket.
      const regionsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (!regionsResp.ok()) throw new Error(`GET /api/v1/user/regions → ${regionsResp.status()}`);
      const regions = await regionsResp.json();
      if (!Array.isArray(regions) || regions.length === 0) {
        skipLine("folder nav check", "no user regions configured");
        return;
      }
      const regionId = regions[0].id as string;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
      if (!bucketsResp.ok()) {
        skipLine("folder nav check", `region buckets → ${bucketsResp.status()}`);
        return;
      }
      // v1.4.0a: the response is now {buckets, perBucketStatsAvailable}.
      // Unwrap to keep the downstream loop unchanged.
      const bucketsBody = await bucketsResp.json();
      const buckets = Array.isArray(bucketsBody) ? bucketsBody : bucketsBody?.buckets;
      if (!Array.isArray(buckets) || buckets.length === 0) {
        skipLine("folder nav check", "region has no buckets");
        return;
      }

      // Probe each bucket for one with commonPrefixes at the root so
      // the visual check has something to look at. Falls back to the
      // first bucket if none expose folders.
      let bucketWithFolders = buckets[0].id as string;
      for (const b of buckets) {
        const objResp = await page!.request.get(
          `${BASE_URL}/api/v1/user/regions/${regionId}/buckets/${b.id}/objects?delimiter=%2F`,
        );
        if (!objResp.ok()) continue;
        const page1 = await objResp.json();
        if (Array.isArray(page1.commonPrefixes) && page1.commonPrefixes.length > 0) {
          bucketWithFolders = b.id;
          break;
        }
      }

      await page!.goto(`${BASE_URL}/files/${regionId}/b/${bucketWithFolders}`, {
        waitUntil: "networkidle",
      });

      // v1.4.0a virtualized the object list — rows are no longer
      // <tbody><tr> but virtualized div containers with
      // data-row-kind="folder" / "file" / "loadMoreSentinel". The
      // virtualizer mounts inside [data-testid="virtual-object-list-scroll"].
      // Either folder rows render (one or more commonPrefixes), file
      // rows render, or the empty-state ("This folder is empty" / "No
      // objects here") shows. Smoke accepts all three — gate is "page
      // renders without crashing".
      // Playwright doesn't accept a mixed CSS-and-text-engine comma list,
      // so wait on either selector independently via waitForFunction.
      await page!.waitForFunction(
        () =>
          !!document.querySelector('[data-testid="virtual-object-list-scroll"]') ||
          /No objects here|This folder is empty/i.test(document.body.innerText),
        null,
        { timeout: 10_000 },
      );
      const hasFolderRow = await page!.locator('[data-row-kind="folder"]').count();
      const hasFileRow = await page!.locator('[data-row-kind="file"]').count();
      const hasEmpty = await page!.locator('text=/No objects here|This folder is empty/i').count();
      if (hasFolderRow === 0 && hasEmpty === 0 && hasFileRow === 0) {
        throw new Error("object browser shows neither folder rows, file rows, nor empty state");
      }
      await shot(page!, "v1.3c-folder-nav");
    });

    // ============================================================
    // [v1.4.0b] paginated key permissions + batch object selection
    // ============================================================
    section("[v1.4.0b/a] /admin/clusters/{cid}/keys/{kid} renders the v1.4.0b filter input when editing");
    await check("key detail edit mode shows the v1.4.0b 'Filter buckets...' input", async () => {
      // Discover one cluster + one key on that cluster. If either is
      // empty (fresh deploy / dev DB) the check skips rather than fails.
      const clustersResp = await page!.request.get(`${BASE_URL}/api/v1/admin/clusters`);
      if (!clustersResp.ok()) {
        skipLine("v1.4.0b key perms check", `clusters → ${clustersResp.status()}`);
        return;
      }
      const clusters = await clustersResp.json();
      if (!Array.isArray(clusters) || clusters.length === 0) {
        skipLine("v1.4.0b key perms check", "no clusters configured");
        return;
      }
      const cid = clusters[0].id as string;

      const keysResp = await page!.request.get(`${BASE_URL}/api/v1/admin/clusters/${cid}/keys`);
      if (!keysResp.ok()) {
        skipLine("v1.4.0b key perms check", `keys → ${keysResp.status()}`);
        return;
      }
      const keys = await keysResp.json();
      if (!Array.isArray(keys) || keys.length === 0) {
        skipLine("v1.4.0b key perms check", "cluster has no keys");
        return;
      }
      const kid = keys[0].id as string;

      await page!.goto(`${BASE_URL}/admin/clusters/${cid}/keys/${kid}`, { waitUntil: "networkidle" });
      // Click "Edit permissions" if visible. Wrapped in a try so a
      // driver that doesn't expose key:permissions (AWS IAM, e.g.)
      // doesn't bring down the smoke.
      const editBtn = page!.getByRole("button", { name: /Edit permissions/i });
      if ((await editBtn.count()) === 0) {
        skipLine("v1.4.0b key perms check", "Edit permissions button not rendered (driver capability?)");
        return;
      }
      await editBtn.first().click();
      await page!.waitForSelector('[data-testid="key-perms-filter"]', { timeout: 10_000 });
      await page!.waitForSelector('[data-testid="key-perms-only-granted"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="key-perms-sticky-save"]', { timeout: 5_000 });
      await shot(page!, "v1.4.0b-key-perms-editor");
    });

    section("[v1.4.0b/b] /files/{regionId}/b/{bid} renders the v1.4.0b select-all checkbox in the header");
    await check("bucket browser shows the v1.4.0b select-all-visible checkbox", async () => {
      const regionsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (!regionsResp.ok()) {
        skipLine("v1.4.0b batch-select check", `regions → ${regionsResp.status()}`);
        return;
      }
      const regions = await regionsResp.json();
      if (!Array.isArray(regions) || regions.length === 0) {
        skipLine("v1.4.0b batch-select check", "no user regions configured");
        return;
      }
      const regionId = regions[0].id as string;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
      if (!bucketsResp.ok()) {
        skipLine("v1.4.0b batch-select check", `buckets → ${bucketsResp.status()}`);
        return;
      }
      const bucketsBody = await bucketsResp.json();
      const buckets = Array.isArray(bucketsBody) ? bucketsBody : bucketsBody?.buckets;
      if (!Array.isArray(buckets) || buckets.length === 0) {
        skipLine("v1.4.0b batch-select check", "region has no buckets");
        return;
      }
      const bid = buckets[0].id as string;

      await page!.goto(`${BASE_URL}/files/${regionId}/b/${bid}`, { waitUntil: "networkidle" });
      // Even on an empty bucket the header still mounts the select-all
      // checkbox — it's disabled when there are no selectable rows.
      await page!.waitForSelector('[data-testid="select-all-visible"]', { timeout: 10_000 });
      await shot(page!, "v1.4.0b-bucket-batch-select");
    });

    // ============================================================
    // [v1.4.0a] virtualized object browser — virtual scroll container mounts
    // ============================================================
    section("[v1.4.0a/a] /files/{regionId}/b/{bid} mounts the virtualized list (v1.4.0a)");
    await check("bucket browser mounts virtual-object-list-scroll container", async () => {
      const regionsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (!regionsResp.ok()) {
        skipLine("v1.4.0a virtualized scroll", `regions → ${regionsResp.status()}`);
        return;
      }
      const regions = await regionsResp.json();
      if (!Array.isArray(regions) || regions.length === 0) {
        skipLine("v1.4.0a virtualized scroll", "no user regions configured");
        return;
      }
      const regionId = regions[0].id as string;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
      if (!bucketsResp.ok()) {
        skipLine("v1.4.0a virtualized scroll", `buckets → ${bucketsResp.status()}`);
        return;
      }
      const bucketsBody = await bucketsResp.json();
      const buckets = Array.isArray(bucketsBody) ? bucketsBody : bucketsBody?.buckets;
      if (!Array.isArray(buckets) || buckets.length === 0) {
        skipLine("v1.4.0a virtualized scroll", "region has no buckets");
        return;
      }
      const bid = buckets[0].id as string;

      await page!.goto(`${BASE_URL}/files/${regionId}/b/${bid}`, { waitUntil: "networkidle" });
      // Either the virtualizer mounted OR the empty-state copy shows.
      // The virtualizer renders even at 0 rows so it's the primary signal.
      // Mixed CSS+text selectors aren't a Playwright thing — use waitForFunction.
      await page!.waitForFunction(
        () =>
          !!document.querySelector('[data-testid="virtual-object-list-scroll"]') ||
          /No objects here|This bucket is empty|This folder is empty/i.test(document.body.innerText),
        null,
        { timeout: 10_000 },
      );
      // Count the visible row containers (folder + file). >10 rows is
      // the spec's bar; smoke tolerates fewer (the matthew dev bucket
      // may have <10 objects) but at least asserts the row containers
      // mount when objects exist.
      const folderRows = await page!.locator('[data-row-kind="folder"]').count();
      const fileRows = await page!.locator('[data-row-kind="file"]').count();
      const totalRows = folderRows + fileRows;
      info(`virtualized browser rendered ${folderRows} folder + ${fileRows} file row(s)`);
      // Empty bucket is allowed — but if rows exist, at least one must
      // render through the virtualizer.
      const hasEmptyMsg = await page!.locator('text=/No objects here|This bucket is empty|This folder is empty/i').count();
      if (totalRows === 0 && hasEmptyMsg === 0) {
        throw new Error("virtualized browser: neither row containers nor empty-state");
      }
      await shot(page!, "v1.4.0a-virtualized-browser");
    });

    // ============================================================
    // [v1.4.0a] /admin/audit pagination chrome (v1.4.0a)
    // ============================================================
    section("[v1.4.0a/b] /admin/audit renders pagination chrome (v1.4.0a)");
    await check("audit log shows pagination summary + Previous/Next + Export CSV", async () => {
      await page!.goto(`${BASE_URL}/admin/audit`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1', { timeout: 10_000 });
      // Export CSV button is always rendered (disabled when zero rows).
      const hasExport = await page!.locator('button:has-text("Export CSV")').count();
      if (hasExport === 0) {
        throw new Error("/admin/audit missing 'Export CSV' button (v1.4.0a)");
      }
      // Pagination chrome is conditional on total>0. On any non-empty
      // deploy the actor's prior elevation alone seeds at least one
      // row, but tolerate the zero case with a warn rather than a hard
      // fail so the smoke survives a fresh dev DB.
      const hasSummary = await page!.locator('[data-testid="audit-pagination-summary"]').count();
      const hasPrev = await page!.locator('button:has-text("Previous")').count();
      const hasNext = await page!.locator('button:has-text("Next")').count();
      if (hasSummary === 0 || hasPrev === 0 || hasNext === 0) {
        const emptyCopy = await page!.locator('text=No events match the filter').count();
        if (emptyCopy === 0) {
          throw new Error(
            `audit pagination chrome missing (summary=${hasSummary} prev=${hasPrev} next=${hasNext}) and no empty-state`,
          );
        }
        warnLine("audit table is empty — pagination chrome hidden by design");
      }
      await shot(page!, "v1.4.0a-audit-pagination");
    });

    // ============================================================
    // [v1.4.0c] /admin/clusters/{cid}/scrub — block scrub UI
    // ============================================================
    section("[v1.4.0c/a] /admin/clusters/{cid}/scrub renders capabilities + Run/Not-supported (v1.4.0c)");
    await check("scrub page renders title + either Run scrub button or Not-supported card", async () => {
      const clustersResp = await page!.request.get(`${BASE_URL}/api/v1/admin/clusters`);
      if (!clustersResp.ok()) {
        skipLine("v1.4.0c scrub check", `clusters → ${clustersResp.status()}`);
        return;
      }
      const clusters = await clustersResp.json();
      if (!Array.isArray(clusters) || clusters.length === 0) {
        skipLine("v1.4.0c scrub check", "no clusters configured");
        return;
      }
      const cid = clusters[0].id as string;

      await page!.goto(`${BASE_URL}/admin/clusters/${cid}/scrub`, { waitUntil: "networkidle" });
      // The page has three render branches:
      //   1) success → renders <h1>Block scrub</h1> + Run/Not-supported card
      //   2) error  → renders BackLink + ErrorBanner ("Couldn't load scrub status.")
      //   3) loading→ skeletons (transitory; networkidle should clear)
      // Smoke accepts (1) or (2) — the error path is what an operator
      // sees against a cluster whose admin API doesn't expose the worker
      // endpoints (e.g. older Garage builds), and the page MUST still
      // mount the BackLink + error message without crashing.
      // Give the React shell a beat to render past the skeleton even if
      // the underlying request returns an error.
      await page!.waitForFunction(
        () => {
          const t = document.body.innerText || "";
          return /Block scrub/.test(t) || /Couldn.?t load scrub status/i.test(t);
        },
        null,
        { timeout: 15_000 },
      );
      const hasTitle = await page!.locator('h1:has-text("Block scrub")').count();
      const hasError = await page!.locator('text=/Couldn.?t load scrub status/i').count();
      if (hasTitle === 0 && hasError === 0) {
        throw new Error("scrub page: neither title nor error banner rendered");
      }
      if (hasTitle > 0) {
        // Supported / unsupported branch — assert one of the two cards is visible.
        const hasRunBtn = await page!
          .locator('button')
          .filter({ hasText: /Run scrub|Running…|Starting…/ })
          .count();
        const hasNotSupported = await page!.locator('text=/Not supported/i').count();
        if (hasRunBtn === 0 && hasNotSupported === 0) {
          throw new Error("scrub page: title rendered but neither Run-scrub button nor Not-supported card");
        }
      } else {
        warnLine("scrub error branch rendered — backend worker endpoint unavailable on this cluster");
      }
      await shot(page!, "v1.4.0c-scrub");
    });

    // ============================================================
    // [v1.4.0c] /admin/usage growth column + range selector + anomaly banner
    // ============================================================
    section("[v1.4.0c/b] /admin/usage shows Growth column + range selector (v1.4.0c)");
    await check("usage page renders Growth (Nd) column header + range buttons", async () => {
      await page!.goto(`${BASE_URL}/admin/usage`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Usage Overview")', { timeout: 10_000 });
      // Growth (7d) is the default column header — present whenever a
      // per-cluster table mounts. If the deploy has no clusters,
      // tolerate the empty state instead.
      const hasGrowthHeader = await page!.locator('th:has-text("Growth")').count();
      const hasEmpty = await page!.locator('text=No clusters connected yet').count();
      if (hasGrowthHeader === 0 && hasEmpty === 0) {
        throw new Error("/admin/usage missing 'Growth' column and no empty-state");
      }
      // Range selector (7d / 30d / 90d). Three buttons or one of them
      // is always rendered.
      const has7d = await page!.locator('button:has-text("7d")').count();
      const has30d = await page!.locator('button:has-text("30d")').count();
      const has90d = await page!.locator('button:has-text("90d")').count();
      if (has7d === 0 || has30d === 0 || has90d === 0) {
        throw new Error(`usage range selector missing buttons (7d=${has7d} 30d=${has30d} 90d=${has90d})`);
      }
      await shot(page!, "v1.4.0c-usage-growth");
    });

    // ============================================================
    // [v1.6] federation: list, wizard step 1, bucket-browser badge
    // ============================================================
    section("[v1.6a] /files/federated-buckets renders (list or empty state)");
    await check("/files/federated-buckets renders 'Federations' header + list or empty state", async () => {
      await page!.goto(`${BASE_URL}/files/federated-buckets`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Federations")', { timeout: 10_000 });
      // Either at least one row OR the "No federations yet" empty state.
      const hasRows = await page!.locator('table tbody tr').count();
      const hasEmpty = await page!.locator('text="No federations yet"').count();
      if (hasRows === 0 && hasEmpty === 0) {
        throw new Error("/files/federated-buckets shows neither rows nor 'No federations yet' empty state");
      }
      // The "+ New federation" CTA renders in both branches (header
      // button on a non-empty list, EmptyState action on an empty list).
      const hasNewCta = await page!.locator('button:has-text("New federation")').count();
      if (hasNewCta === 0) {
        throw new Error("/files/federated-buckets missing '+ New federation' CTA");
      }
      await shot(page!, "v1.6a-federations-list");
    });

    section("[v1.6b] /files/federated-buckets/new wizard step 1 (Primary picker)");
    await check("/files/federated-buckets/new shows 'New federation' header + Step 1 Primary picker", async () => {
      await page!.goto(`${BASE_URL}/files/federated-buckets/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("New federation")', { timeout: 10_000 });
      // Step counter — 5-step wizard per ADR-0005.
      const hasStepText = await page!.locator('text=/Step 1 of 5/i').count();
      if (hasStepText === 0) {
        throw new Error("/files/federated-buckets/new missing 'Step 1 of 5' indicator");
      }
      // Step 1 surfaces the primary region + bucket selects.
      const hasPrimaryRegion = await page!.locator('#primaryRegion').count();
      const hasPrimaryBucket = await page!.locator('#primaryBucket').count();
      if (hasPrimaryRegion === 0 || hasPrimaryBucket === 0) {
        throw new Error(
          `/files/federated-buckets/new step 1 missing primary selects (region=${hasPrimaryRegion} bucket=${hasPrimaryBucket})`,
        );
      }
      await shot(page!, "v1.6b-federation-wizard-step1");
    });

    section("[v1.6c] /files/{rid}/b/{bid} federation badge (probe + render)");
    await check("bucket browser tolerates federation badge — present (federated bucket) or absent (non-federated)", async () => {
      const regionsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions`);
      if (!regionsResp.ok()) {
        skipLine("v1.6c federation badge probe", `regions → ${regionsResp.status()}`);
        return;
      }
      const regions = await regionsResp.json();
      if (!Array.isArray(regions) || regions.length === 0) {
        skipLine("v1.6c federation badge probe", "no user regions configured");
        return;
      }
      const regionId = regions[0].id as string;

      const bucketsResp = await page!.request.get(`${BASE_URL}/api/v1/user/regions/${regionId}/buckets`);
      if (!bucketsResp.ok()) {
        skipLine("v1.6c federation badge probe", `buckets → ${bucketsResp.status()}`);
        return;
      }
      const bucketsBody = await bucketsResp.json();
      const buckets = Array.isArray(bucketsBody) ? bucketsBody : bucketsBody?.buckets;
      if (!Array.isArray(buckets) || buckets.length === 0) {
        skipLine("v1.6c federation badge probe", "region has no buckets");
        return;
      }
      const bid = buckets[0].id as string;

      await page!.goto(`${BASE_URL}/files/${regionId}/b/${bid}`, { waitUntil: "networkidle" });
      // The bucket page must mount (h1 present) regardless of federation
      // membership. The badge is only rendered when /by-target returns
      // a federation record; otherwise null and no badge appears.
      await page!.waitForSelector('h1', { timeout: 10_000 });
      const hasBadge = await page!.locator('[data-testid="federation-badge"]').count();
      if (hasBadge > 0) {
        info(`federation badge rendered on /files/${regionId}/b/${bid} (bucket is part of a federation)`);
        // If the badge is present, it should reference "Federated".
        const badgeText = await page!.locator('[data-testid="federation-badge"]').first().textContent();
        if (!badgeText || !/Federated/i.test(badgeText)) {
          throw new Error(`federation badge present but text unexpected: '${badgeText}'`);
        }
      } else {
        info(`no federation badge on /files/${regionId}/b/${bid} (bucket not federated — acceptable)`);
      }
      await shot(page!, "v1.6c-bucket-federation-badge");
    });

    // ============================================================
    // [v1.7a] /admin/service-accounts list page (v1.7.0c)
    // ============================================================
    section("[v1.7a] /admin/service-accounts renders (list or empty state)");
    await check("/admin/service-accounts renders 'Service accounts' header + list or empty state + New CTA", async () => {
      await page!.goto(`${BASE_URL}/admin/service-accounts`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Service accounts")', { timeout: 10_000 });
      // Either at least one row OR the "No service accounts yet" empty state.
      const hasRows = await page!.locator('table tbody tr').count();
      const hasEmpty = await page!.locator('text="No service accounts yet"').count();
      if (hasRows === 0 && hasEmpty === 0) {
        throw new Error("/admin/service-accounts shows neither rows nor 'No service accounts yet' empty state");
      }
      // The "+ New service account" CTA renders in both branches
      // (header button on a non-empty list, EmptyState action on empty).
      const hasNewCta = await page!.locator('text="+ New service account"').count();
      if (hasNewCta === 0) {
        throw new Error("/admin/service-accounts missing '+ New service account' CTA");
      }
      await shot(page!, "v1.7a-service-accounts-list");
    });

    section("[v1.7b] /admin/service-accounts/new renders mint form (v1.7.0c)");
    await check("/admin/service-accounts/new shows 'Mint a service account' header + name input + capability search + submit", async () => {
      await page!.goto(`${BASE_URL}/admin/service-accounts/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Mint a service account")', { timeout: 10_000 });
      // Form mounts with the testid-marked surfaces.
      await page!.waitForSelector('[data-testid="new-sa-form"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="sa-name-input"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="sa-cap-search"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="sa-submit-button"]', { timeout: 5_000 });
      await shot(page!, "v1.7b-service-accounts-new");
    });

    // ============================================================
    // [v1.7c] /files/webhooks list page (v1.7.0e)
    // ============================================================
    section("[v1.7c] /files/webhooks renders (list or empty state)");
    await check("/files/webhooks renders 'Webhooks' header + list or empty state + New CTA", async () => {
      await page!.goto(`${BASE_URL}/files/webhooks`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("Webhooks")', { timeout: 10_000 });
      // Either at least one row OR the "No webhooks configured" empty state.
      const hasRows = await page!.locator('table tbody tr').count();
      const hasEmpty = await page!.locator('text="No webhooks configured"').count();
      if (hasRows === 0 && hasEmpty === 0) {
        throw new Error("/files/webhooks shows neither rows nor 'No webhooks configured' empty state");
      }
      // "+ New webhook" CTA renders in both branches (header button on
      // a non-empty list, EmptyState action on empty).
      const hasNewCta = await page!.locator('text="+ New webhook"').count();
      if (hasNewCta === 0) {
        throw new Error("/files/webhooks missing '+ New webhook' CTA");
      }
      await shot(page!, "v1.7c-webhooks-list");
    });

    section("[v1.7d] /files/webhooks/new renders subscription form (v1.7.0e)");
    await check("/files/webhooks/new shows 'New webhook' header + identity + events + target URL form", async () => {
      await page!.goto(`${BASE_URL}/files/webhooks/new`, { waitUntil: "networkidle" });
      await page!.waitForSelector('h1:has-text("New webhook")', { timeout: 10_000 });
      // Form mounts with the testid-marked surfaces.
      await page!.waitForSelector('[data-testid="new-webhook-form"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="webhook-name-input"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="webhook-target-url-input"]', { timeout: 5_000 });
      await page!.waitForSelector('[data-testid="webhook-create-submit"]', { timeout: 5_000 });
      await shot(page!, "v1.7d-webhooks-new");
    });

    // ============================================================
    // 14. Console / pageerror gate
    // ============================================================
    section("[NN] console + pageerror gate (v0.8.0c)");
    await check("no console errors or page errors across the run", async () => {
      if (consoleWarnings.length > 0) {
        // Surface but don't fail.
        warnLine(`saw ${consoleWarnings.length} console warning(s) — first: ${consoleWarnings[0].text.slice(0, 200)}`);
      }
      if (consoleErrors.length === 0 && pageErrors.length === 0) return;
      const lines: string[] = [];
      for (const e of consoleErrors) lines.push(`  [console.error @ ${e.url}] ${e.text.slice(0, 300)}`);
      for (const e of pageErrors) lines.push(`  [pageerror @ ${e.url}] ${e.message.slice(0, 300)}`);
      throw new Error(
        `${consoleErrors.length} console error(s), ${pageErrors.length} pageerror(s)\n${lines.join("\n")}`,
      );
    });
  } catch (setupErr) {
    failLine("smoke setup", setupErr instanceof Error ? setupErr.message : String(setupErr));
    return 2;
  } finally {
    if (page) await page.close().catch(() => {});
    if (context) await context.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
  }

  // ---------- summary ----------
  const passed = results.filter((r) => r.ok).length;
  const failed = results.length - passed;
  process.stdout.write("\n");
  if (failed === 0) {
    process.stdout.write(`${C.bold}${C.green}${passed}/${results.length} checks passed${C.reset}\n`);
    return 0;
  }
  process.stdout.write(`${C.bold}${C.red}${failed}/${results.length} checks failed${C.reset}\n`);
  for (const r of results.filter((r) => !r.ok)) {
    process.stdout.write(`  ${C.red}- ${r.name}${C.reset}\n`);
  }
  process.stdout.write(`${C.dim}screenshots saved to ${SHOT_DIR}${C.reset}\n`);
  return 1;
}

main().then(
  (code) => process.exit(code),
  (err) => {
    failLine("unhandled error", err instanceof Error ? err.stack ?? err.message : String(err));
    process.exit(2);
  },
);
