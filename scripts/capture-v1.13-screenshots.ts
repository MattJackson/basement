#!/usr/bin/env node
// capture-v1.13-screenshots.ts — Playwright capture of v1.13 skins feature
// launch-readiness screens. Companion to capture-v1.11-screenshots.ts.
//
// v1.13 ships the pluggable skin system (v1.13.0a/b/c) which is the
// one new operator-facing UI surface in this minor. The system includes:
// - Admin skins management page (/admin/system) with upload/activate/delete
// - Per-user skin selector in user menu when org policy permits
// - FE rendering of typography, density, borderRadius, footer, loginHero
//
// Usage:
//   node scripts/capture-v1.13-screenshots.ts
//   BASE_URL=https://basement.pq.io \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/capture-v1.13-screenshots.ts
//
// Outputs:
//   docs/screenshots/v1.13/01-admin-system-skins.png

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
      `       install with: pnpm -C frontend install\n`,
  );
  process.exit(2);
}

const { chromium } = (await import(pathToFileURL(PLAYWRIGHT_INDEX).href)) as { chromium: typeof ChromiumApi };

const BASE_URL = (process.env.BASE_URL ?? "https://basement.pq.io").replace(/\/$/, "");
const USERNAME = process.env.BUI_USERNAME ?? process.env.BASEMENT_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? process.env.BASEMENT_PASSWORD ?? process.env.PASSWORD ?? "password";

const OUT_DIR = resolve(__dirname, "..", "docs", "screenshots", "v1.13");
mkdirSync(OUT_DIR, { recursive: true });

async function loginAndElevate(ctx: BrowserContext): Promise<void> {
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
  // Elevate so /admin/ is unblocked.
  const page = await ctx.newPage();
  const elevateResp = await page.request.post(`${BASE_URL}/api/v1/auth/elevate`, {
    headers: { "Content-Type": "application/json" },
    data: { target_mode: "admin", password: PASSWORD },
  });
  if (!elevateResp.ok()) {
    throw new Error(`POST /api/v1/auth/elevate -> ${elevateResp.status()} ${await elevateResp.text()}`);
  }
  await page.close();
}

async function shotFullPage(page: Page, name: string): Promise<void> {
  await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
  const path = join(OUT_DIR, `${name}.png`);
  await page.screenshot({ path, fullPage: true });
  process.stdout.write(`  -> ${path}\n`);
}

async function main(): Promise<number> {
  process.stdout.write(`v1.13 capture against ${BASE_URL}\n`);
  process.stdout.write(`out: ${OUT_DIR}\n`);

  let browser: Browser | undefined;
  let ctx: BrowserContext | undefined;
  try {
    browser = await chromium.launch({ headless: true });
    ctx = await browser.newContext({
      viewport: { width: 1440, height: 900 },
      ignoreHTTPSErrors: false,
    });
    await loginAndElevate(ctx);

    const page = await ctx.newPage();

    // 01 — /admin/system with Skins section visible
    // The skins management card lives below the Gateways card on the system page.
    await page.goto(`${BASE_URL}/admin/system`, { waitUntil: "networkidle" });
    await page
      .waitForSelector(
        '[data-testid="gateways-card"], [data-testid="admin-session-ttl-card"]',
        { timeout: 10_000 },
      )
      .catch(() => {});
    // Scroll to skins section if needed
    await page.evaluate(() => {
      const gateways = document.querySelector('[data-testid="gateways-card"]');
      if (gateways) {
        gateways.scrollIntoView({ behavior: "auto", block: "end" });
      }
    });
    // Wait for skins card to be visible
    await page.waitForSelector('[data-testid="system-skins-card"], h3:has-text("Skins")', { timeout: 10_000 }).catch(() => {});
    await shotFullPage(page, "01-admin-system-skins");

    await page.close();
    return 0;
  } catch (err) {
    process.stderr.write(`[FAIL] ${err instanceof Error ? err.message : String(err)}\n`);
    return 1;
  } finally {
    await ctx?.close().catch(() => {});
    await browser?.close().catch(() => {});
  }
}

process.exit(await main());
