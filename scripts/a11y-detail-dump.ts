#!/usr/bin/env node
// scripts/a11y-detail-dump.ts — dumps detailed axe-core violations
// (per element, with color info) for a focused subset of routes,
// helping triage which specific elements/tokens fail color-contrast.
//
// Output: /tmp/basement/a11y-detail-{ts}.json with full per-element
// violation data.

import type { Page, chromium as ChromiumApi } from "playwright";
import { existsSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const FRONTEND_NM = resolve(__dirname, "..", "frontend", "node_modules");
const PLAYWRIGHT = join(FRONTEND_NM, "playwright", "index.mjs");
const AXE = join(FRONTEND_NM, "@axe-core", "playwright", "dist", "index.js");

const { chromium } = (await import(pathToFileURL(PLAYWRIGHT).href)) as { chromium: typeof ChromiumApi };
const { default: AxeBuilder } = (await import(pathToFileURL(AXE).href)) as { default: any };

const BASE_URL = process.env.BASE_URL ?? "https://basement.pq.io";
const USERNAME = process.env.BUI_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? "password";

const ROUTES = [
  "/", "/login", "/admin/login",
  "/files", "/files/keys", "/files/shares", "/files/syncs",
  "/files/backups", "/files/backups/new", "/files/federated-buckets",
  "/files/webhooks", "/files/webhooks/new",
  "/admin/system", "/admin/users", "/admin/clusters", "/admin/audit",
  "/admin/service-accounts", "/admin/policies", "/admin/migrate",
  "/admin/usage",
];

async function loginAndSetRole(ctx: any) {
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

  const elevateResp = await ctx.request.post(`${BASE_URL}/api/v1/auth/elevate`, {
    headers: { "Content-Type": "application/json" },
    data: { target_mode: "admin", password: PASSWORD },
  });
  if (!elevateResp.ok()) {
    throw new Error(`POST /api/v1/auth/elevate → ${elevateResp.status()} ${await elevateResp.text()}`);
  }

  await ctx.request.put(`${BASE_URL}/api/v1/auth/active-role`, {
    headers: { "Content-Type": "application/json" },
    data: { kind: "ui-admin" },
  });
}

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  await loginAndSetRole(ctx);
  const page = await ctx.newPage();

  const allViolations: any[] = [];
  for (const route of ROUTES) {
    try {
      await page.goto(`${BASE_URL}${route}`, { waitUntil: "networkidle", timeout: 15_000 });
      const audit = await new AxeBuilder({ page }).analyze();
      for (const v of audit.violations) {
        for (const node of v.nodes) {
          allViolations.push({
            route,
            rule: v.id,
            impact: v.impact,
            target: node.target,
            html: node.html.slice(0, 300),
            failureSummary: node.failureSummary,
            colorInfo: (node as any).any?.find((a: any) => a.id === "color-contrast")?.data ?? null,
          });
        }
      }
    } catch (e) {
      console.error(`route ${route}:`, e);
    }
  }

  const outPath = `/tmp/basement/a11y-detail-${Date.now()}.json`;
  mkdirSync("/tmp/basement", { recursive: true });
  writeFileSync(outPath, JSON.stringify(allViolations, null, 2));
  console.log(`wrote ${allViolations.length} violation entries to ${outPath}`);
  await browser.close();
})();
