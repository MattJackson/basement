#!/usr/bin/env node
// mobile-audit.ts — multi-viewport mobile-friendliness audit for a live
// basement deployment.
//
// Sibling of scripts/comprehensive-smoke.ts. Where comprehensive-smoke
// is the systematic CRUD/coverage walk (one desktop viewport + one
// mobile viewport, every route, every form), this script is the
// dedicated mobile-quality probe: it visits every route at 4 mobile +
// tablet viewports and runs a battery of mobile-specific checks
// (horizontal scroll, tap-target sizes, sticky-header overlap, console
// errors) at each one.
//
// v1.11.0.17 cycle: ships alongside an inline-fix batch for the
// obvious bugs the audit surfaces.
//
// =========================================================================
// SAFETY GUARANTEES
// =========================================================================
//
// READ-ONLY. This audit never POSTs/PUTs/PATCHes/DELETEs anything on
// the target deployment. It only logs in (to elevate cookies into the
// browser context), elevates to admin (so admin routes render), and
// GETs every route. No ephemeral resources are created. No operator
// data is touched. The comprehensive-smoke harness handles destructive
// CRUD coverage with its own ephemeral safety layer; this script is
// strictly the eyes, not the hands.
//
// =========================================================================
// USAGE
// =========================================================================
//
//   node scripts/mobile-audit.ts
//   BASE_URL=https://basement.example.com \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/mobile-audit.ts
//
// Output:
//   - Screenshots: /tmp/basement-mobile-{ts}/{viewport}/{route}.png
//   - Console: green [ok] / yellow [MINOR] / red [MAJOR] / [skip]
//   - Markdown report: docs/mobile-audit-{YYYY-MM-DD}.md
//
// Exit codes:
//   0  audit ran to completion (findings reported regardless of severity)
//   1  the audit harness itself failed (login, browser launch, etc.)
//   2  bad invocation / setup error
//
// Findings are signal, not gates — the script always exits 0 if it
// completed the walk, so CI can collect the report without flapping
// on a single regression.

import type { Browser, BrowserContext, ConsoleMessage, Page, chromium as ChromiumApi } from "playwright";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
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
const TODAY = new Date().toISOString().slice(0, 10);
const SHOT_ROOT = process.env.MOBILE_SHOT_DIR ?? join("/tmp", `basement-mobile-${RUN_TS}`);
mkdirSync(SHOT_ROOT, { recursive: true });

// Where the markdown report lands. Per-cycle convention:
//   docs/mobile-audit-{YYYY-MM-DD}.md
const REPORT_PATH = resolve(__dirname, "..", "docs", `mobile-audit-${TODAY}.md`);

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
function passLine(name: string) {
  process.stdout.write(`${C.green}[ok]${C.reset} ${name}\n`);
}
function minorLine(name: string, detail: string) {
  process.stdout.write(`${C.yellow}[MINOR]${C.reset} ${name} ${C.dim}${detail}${C.reset}\n`);
}
function majorLine(name: string, detail: string) {
  process.stderr.write(`${C.red}[MAJOR]${C.reset} ${name} ${C.dim}${detail}${C.reset}\n`);
}
function skipLine(name: string, reason: string) {
  process.stdout.write(`${C.yellow}[skip]${C.reset} ${name} ${C.dim}(${reason})${C.reset}\n`);
}
function warnLine(msg: string) {
  process.stderr.write(`${C.yellow}[warn]${C.reset} ${msg}\n`);
}

// ---------- viewport profiles ----------
type ViewportProfile = {
  key: string;
  label: string;
  width: number;
  height: number;
  isMobile: boolean;
  hasTouch: boolean;
};

const VIEWPORTS: ViewportProfile[] = [
  { key: "iphone-se", label: "iPhone SE (375x667)", width: 375, height: 667, isMobile: true, hasTouch: true },
  { key: "iphone-14", label: "iPhone 14 (390x844)", width: 390, height: 844, isMobile: true, hasTouch: true },
  { key: "ipad-mini", label: "iPad Mini (768x1024)", width: 768, height: 1024, isMobile: true, hasTouch: true },
  { key: "android-narrow", label: "Android narrow (360x640)", width: 360, height: 640, isMobile: true, hasTouch: true },
];

// ---------- route table ----------
// Mirrors scripts/comprehensive-smoke.ts route enumeration; kept in
// sync by convention (any new route added there should be added here
// too). The audit walks every route at every viewport, which is why
// the list omits the ephemeral CRUD URLs the smoke uses for fixtures.
type RouteSpec = {
  path: string;
  key: string;
  requiresAdmin?: boolean;
  requiresRegion?: boolean;
  requiresBucket?: boolean;
  requiresCluster?: boolean;
};

const ROUTES: RouteSpec[] = [
  // Public / unauthenticated
  { path: "/admin/login", key: "admin-login" },
  { path: "/share/notarealtokenforsmoke", key: "share-bogus-token" },

  // User shell
  { path: "/", key: "root" },
  { path: "/files", key: "files-home" },
  { path: "/files/keys", key: "files-keys" },
  { path: "/files/keys/new", key: "files-keys-new" },
  { path: "/files/regions/new", key: "files-regions-new" },
  { path: "/files/shares", key: "files-shares" },
  { path: "/files/syncs", key: "files-syncs" },
  { path: "/files/syncs/new", key: "files-syncs-new" },
  { path: "/files/backups", key: "files-backups" },
  { path: "/files/backups/new", key: "files-backups-new" },
  { path: "/files/federated-buckets", key: "files-federated" },
  { path: "/files/federated-buckets/new", key: "files-federated-new" },
  { path: "/files/webhooks", key: "files-webhooks" },
  { path: "/files/webhooks/new", key: "files-webhooks-new" },

  // User shell w/ discovered params
  { path: "/files/{regionId}", key: "files-region", requiresRegion: true },
  { path: "/files/{regionId}/b/{bid}", key: "files-region-bucket", requiresRegion: true, requiresBucket: true },

  // Admin
  { path: "/admin", key: "admin-root", requiresAdmin: true },
  { path: "/admin/system", key: "admin-system", requiresAdmin: true },
  { path: "/admin/users", key: "admin-users", requiresAdmin: true },
  { path: "/admin/users/new", key: "admin-users-new", requiresAdmin: true },
  { path: "/admin/clusters", key: "admin-clusters", requiresAdmin: true },
  { path: "/admin/clusters/new", key: "admin-clusters-new", requiresAdmin: true },
  { path: "/admin/buckets", key: "admin-buckets", requiresAdmin: true },
  { path: "/admin/audit", key: "admin-audit", requiresAdmin: true },
  { path: "/admin/policies", key: "admin-policies", requiresAdmin: true },
  { path: "/admin/service-accounts", key: "admin-sa", requiresAdmin: true },
  { path: "/admin/service-accounts/new", key: "admin-sa-new", requiresAdmin: true },
  { path: "/admin/migrate", key: "admin-migrate", requiresAdmin: true },
  { path: "/admin/usage", key: "admin-usage", requiresAdmin: true },

  // Admin w/ cluster param
  { path: "/admin/clusters/{cid}", key: "admin-cluster-detail", requiresAdmin: true, requiresCluster: true },
  { path: "/admin/clusters/{cid}/edit", key: "admin-cluster-edit", requiresAdmin: true, requiresCluster: true },
  { path: "/admin/clusters/{cid}/layout", key: "admin-cluster-layout", requiresAdmin: true, requiresCluster: true },
  { path: "/admin/clusters/{cid}/scrub", key: "admin-cluster-scrub", requiresAdmin: true, requiresCluster: true },
];

// ---------- auth helpers (cloned from comprehensive-smoke) ----------
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

// ---------- finding model ----------
type Severity = "MAJOR" | "MINOR";
type Finding = {
  viewport: string;
  route: string;
  severity: Severity;
  category: string;
  detail: string;
};
const findings: Finding[] = [];
function report(severity: Severity, viewport: string, route: string, category: string, detail: string) {
  findings.push({ viewport, route, severity, category, detail });
  const tag = `[${severity}] ${viewport} ${route} (${category})`;
  if (severity === "MAJOR") majorLine(tag, detail);
  else minorLine(tag, detail);
}

// ---------- console error tracking ----------
const consoleErrorsByRoute = new Map<string, string[]>();
function attachListeners(page: Page, viewportKey: string) {
  page.on("console", (msg: ConsoleMessage) => {
    if (msg.type() !== "error") return;
    const text = msg.text();
    // Low-signal browser failures we can't act on.
    if (text.includes("Failed to load resource")) return;
    const key = `${viewportKey}::${page.url()}`;
    const arr = consoleErrorsByRoute.get(key) ?? [];
    arr.push(text);
    consoleErrorsByRoute.set(key, arr);
  });
  page.on("pageerror", (err: Error) => {
    const key = `${viewportKey}::${page.url()}`;
    const arr = consoleErrorsByRoute.get(key) ?? [];
    arr.push(`${err.name}: ${err.message}`);
    consoleErrorsByRoute.set(key, arr);
  });
}

// ---------- per-route checks ----------
//
// Each check runs against an already-loaded page at a given viewport.
// Findings go into `findings[]` keyed by viewport+route+category for
// the final markdown report. Checks are designed to be conservative —
// they flag only things that would visibly break the operator's day,
// not pixel-perfect ideals.

type CheckCtx = {
  page: Page;
  viewport: ViewportProfile;
  route: RouteSpec;
  url: string;
};

// 1. No horizontal scroll. document.documentElement.scrollWidth must
//    not exceed the viewport width by more than 1px (sub-pixel rounding).
async function checkNoHorizontalScroll(ctx: CheckCtx): Promise<void> {
  const result = await ctx.page.evaluate(() => {
    const scrollW = document.documentElement.scrollWidth;
    const clientW = document.documentElement.clientWidth;
    const bodyScrollW = document.body.scrollWidth;
    return { scrollW, clientW, bodyScrollW };
  });
  // Allow 1px slop for sub-pixel rounding. Anything more is real.
  const overflow = Math.max(result.scrollW, result.bodyScrollW) - result.clientW;
  if (overflow > 1) {
    // Identify the worst offender so an operator has a starting point.
    const offender = await ctx.page.evaluate((vw: number) => {
      const all = Array.from(document.querySelectorAll<HTMLElement>("*"));
      let worst: { tag: string; cls: string; right: number } | null = null;
      for (const el of all) {
        const r = el.getBoundingClientRect();
        if (r.right > vw + 1) {
          if (!worst || r.right > worst.right) {
            worst = {
              tag: el.tagName.toLowerCase(),
              cls: (el.className || "").toString().slice(0, 80),
              right: Math.round(r.right),
            };
          }
        }
      }
      return worst;
    }, ctx.viewport.width);
    const offenderStr = offender
      ? ` worst: <${offender.tag} class="${offender.cls}"> right=${offender.right}px`
      : "";
    report(
      "MAJOR",
      ctx.viewport.key,
      ctx.route.path,
      "horizontal-scroll",
      `page width ${Math.max(result.scrollW, result.bodyScrollW)}px > viewport ${result.clientW}px (overflow ${overflow}px).${offenderStr}`,
    );
  }
}

// 2. Tap targets ≥44×44px. Apple HIG + WCAG 2.5.5 (Level AAA target).
//    Inspect every visible <a>, <button>, <input>, <select>, <textarea>,
//    and [role="button"]; flag any whose bounding rect is < 44 in
//    either dimension. Disabled / hidden / off-screen ones are skipped.
async function checkTapTargets(ctx: CheckCtx): Promise<void> {
  // Skip tap-target check on tablet+ viewports — pointer is closer to
  // desktop precision there, and the 44px floor is a touch-phone HIG.
  if (ctx.viewport.width >= 700) return;
  const small = await ctx.page.evaluate(() => {
    const SEL = 'a, button, input:not([type="hidden"]), select, textarea, [role="button"], [role="link"], [role="menuitem"]';
    const targets = Array.from(document.querySelectorAll<HTMLElement>(SEL));
    const offenders: { tag: string; text: string; cls: string; w: number; h: number }[] = [];
    for (const el of targets) {
      // Skip disabled / hidden / off-screen.
      if ((el as HTMLInputElement).disabled) continue;
      const cs = window.getComputedStyle(el);
      if (cs.display === "none" || cs.visibility === "hidden") continue;
      // Skip elements with aria-hidden=true (icon decoration inside a parent button).
      if (el.getAttribute("aria-hidden") === "true") continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      // Off-screen above (modal in background, drawer not yet open).
      if (r.bottom < 0 || r.right < 0) continue;
      // Skip checkboxes / radios — these are nearly always wrapped in
      // larger labels and the native control rendering doesn't itself
      // need the 44px floor.
      const tName = el.tagName.toLowerCase();
      const inputType = tName === "input" ? (el as HTMLInputElement).type : "";
      if (tName === "input" && (inputType === "checkbox" || inputType === "radio")) continue;
      // Skip very small inline links that live INSIDE a paragraph of
      // text — these are reading-flow links (e.g. "see docs"), not
      // primary controls, and inflating them to 44px breaks copy
      // readability. Heuristic: link whose parent is a <p> / <li>
      // with sibling text content.
      if ((tName === "a" || el.getAttribute("role") === "link") && el.parentElement) {
        const parent = el.parentElement;
        const parentTag = parent.tagName.toLowerCase();
        const hasSiblingText = (parent.textContent || "").trim().length > (el.textContent || "").trim().length + 10;
        if ((parentTag === "p" || parentTag === "li" || parentTag === "span") && hasSiblingText) continue;
      }
      if (r.width < 44 || r.height < 44) {
        offenders.push({
          tag: tName + (inputType ? `[type=${inputType}]` : ""),
          text: (el.textContent || "").trim().slice(0, 40),
          cls: (el.className || "").toString().slice(0, 60),
          w: Math.round(r.width),
          h: Math.round(r.height),
        });
      }
    }
    return offenders;
  });
  if (small.length === 0) return;
  // Cap per-route detail at 5 offenders — beyond that the bug is
  // systemic (some shared component) and the operator only needs the
  // pattern, not a flood.
  const top = small.slice(0, 5);
  const detail = top
    .map((o) => `<${o.tag}> ${o.w}x${o.h}px "${o.text || "<no-text>"}"`)
    .join("; ");
  const more = small.length > top.length ? ` (+${small.length - top.length} more)` : "";
  report(
    "MINOR",
    ctx.viewport.key,
    ctx.route.path,
    "tap-target",
    `${small.length} interactive element(s) < 44x44px. ${detail}${more}`,
  );
}

// 3. No content cut off by the sticky header. The header is sticky top-0
//    with h-16 (64px). Confirm the first <h1> isn't behind the header.
async function checkHeaderOverlap(ctx: CheckCtx): Promise<void> {
  const result = await ctx.page.evaluate(() => {
    const header = document.querySelector("header");
    const h1 = document.querySelector("h1");
    if (!header || !h1) return null;
    const hr = header.getBoundingClientRect();
    const tr = h1.getBoundingClientRect();
    return { headerBottom: hr.bottom, h1Top: tr.top, h1Bottom: tr.bottom };
  });
  if (!result) return;
  // h1 should start at or below the bottom of the sticky header at
  // initial scroll position. Allow 4px slop.
  if (result.h1Top < result.headerBottom - 4) {
    report(
      "MAJOR",
      ctx.viewport.key,
      ctx.route.path,
      "header-overlap",
      `<h1> top=${Math.round(result.h1Top)}px is above header bottom=${Math.round(result.headerBottom)}px (sticky header obscures content)`,
    );
  }
}

// 4. Console errors on the route. Counted via the page-level listener
//    above; emitted as a finding for any route that produced > 0.
function checkConsoleErrors(ctx: CheckCtx): void {
  const key = `${ctx.viewport.key}::${ctx.page.url()}`;
  const errs = consoleErrorsByRoute.get(key) ?? [];
  if (errs.length === 0) return;
  // Cap detail at 3 errors — the rest are usually duplicates.
  const sample = errs.slice(0, 3).map((e) => e.slice(0, 120)).join(" | ");
  report(
    "MINOR",
    ctx.viewport.key,
    ctx.route.path,
    "console-error",
    `${errs.length} console error(s). Sample: ${sample}`,
  );
}

// 5. Primary nav is reachable. UserShell + AppShell both render a
//    <nav aria-label="Primary">. Confirm at least one nav link is
//    visible within the viewport (or the nav is horizontally
//    scrollable, per the UserShell v1.8.0e pattern).
async function checkPrimaryNavReachable(ctx: CheckCtx): Promise<void> {
  // Skip on public/unauth pages where the shells aren't mounted.
  if (ctx.route.path.startsWith("/admin/login") || ctx.route.path.startsWith("/share/")) return;
  const result = await ctx.page.evaluate(() => {
    const nav = document.querySelector<HTMLElement>('nav[aria-label="Primary"]');
    if (!nav) return { found: false, scrollable: false, visibleLinks: 0 };
    const cs = window.getComputedStyle(nav);
    const scrollable =
      (cs.overflowX === "auto" || cs.overflowX === "scroll") && nav.scrollWidth > nav.clientWidth;
    const fits = nav.scrollWidth <= nav.clientWidth + 1;
    const links = Array.from(nav.querySelectorAll<HTMLElement>("a, button"));
    let visibleLinks = 0;
    for (const l of links) {
      const r = l.getBoundingClientRect();
      if (r.width > 0 && r.height > 0 && r.left < window.innerWidth) visibleLinks++;
    }
    return { found: true, scrollable: scrollable || fits, visibleLinks };
  });
  if (!result.found) {
    // Many admin sub-routes don't have the Primary nav (e.g. cluster
    // edit screens render under AppShell's primary nav, which IS
    // present — but defensively, only flag if the route is expected
    // to have it). Skip silently.
    return;
  }
  if (!result.scrollable) {
    report(
      "MAJOR",
      ctx.viewport.key,
      ctx.route.path,
      "nav-overflow",
      `Primary nav overflows viewport but isn't horizontally scrollable`,
    );
  }
  if (result.visibleLinks === 0) {
    report(
      "MAJOR",
      ctx.viewport.key,
      ctx.route.path,
      "nav-empty",
      `Primary nav has no visible links at this viewport`,
    );
  }
}

// 6. Forms are usable: every <input>/<select>/<textarea> in a <form>
//    has a visible <label> (either wrapping it, with for=id, or
//    aria-label). Catches the "field with placeholder but no label"
//    pattern that hurts mobile autofill + accessibility.
async function checkFormsLabeled(ctx: CheckCtx): Promise<void> {
  // Only audit forms on routes whose path ends in /new or /edit, where
  // user input is the primary purpose. Other routes may have search
  // boxes etc. that aren't the page's job.
  if (!/\/(new|edit)(\?|$)/.test(ctx.url) && !/\/login(\?|$)/.test(ctx.url)) return;
  const result = await ctx.page.evaluate(() => {
    const orphans: { tag: string; type: string; name: string; placeholder: string }[] = [];
    const forms = document.querySelectorAll("form");
    for (const form of forms) {
      const inputs = form.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
        "input, select, textarea",
      );
      for (const el of inputs) {
        if (el.tagName === "INPUT" && ["hidden", "submit", "button", "reset"].includes((el as HTMLInputElement).type)) continue;
        if ((el as HTMLInputElement).disabled) continue;
        if (el.getAttribute("aria-hidden") === "true") continue;
        const cs = window.getComputedStyle(el);
        if (cs.display === "none" || cs.visibility === "hidden") continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        // Label resolution: aria-label, aria-labelledby, wrapping <label>, label[for=id].
        if (el.getAttribute("aria-label")?.trim()) continue;
        if (el.getAttribute("aria-labelledby")?.trim()) continue;
        if (el.closest("label")) continue;
        if (el.id) {
          const lbl = document.querySelector(`label[for="${CSS.escape(el.id)}"]`);
          if (lbl) continue;
        }
        orphans.push({
          tag: el.tagName.toLowerCase(),
          type: (el as HTMLInputElement).type ?? "",
          name: (el as HTMLInputElement).name ?? "",
          placeholder: (el as HTMLInputElement).placeholder ?? "",
        });
      }
    }
    return orphans;
  });
  if (result.length === 0) return;
  const detail = result
    .slice(0, 4)
    .map((o) => `<${o.tag}${o.type ? `[type=${o.type}]` : ""} name="${o.name}" placeholder="${o.placeholder}">`)
    .join("; ");
  const more = result.length > 4 ? ` (+${result.length - 4} more)` : "";
  report(
    "MINOR",
    ctx.viewport.key,
    ctx.route.path,
    "form-label",
    `${result.length} form input(s) without a resolvable label. ${detail}${more}`,
  );
}

// 7. Modal dismissibility. If a [role="dialog"] is open on this route,
//    confirm there's a close affordance (button with aria-label close,
//    or an X icon button). Many of these routes don't have dialogs;
//    that's fine — the check is a no-op when none are open.
async function checkModalDismissible(ctx: CheckCtx): Promise<void> {
  const result = await ctx.page.evaluate(() => {
    const dialogs = Array.from(document.querySelectorAll<HTMLElement>('[role="dialog"]'));
    const issues: { hasClose: boolean; ariaLabel: string }[] = [];
    for (const d of dialogs) {
      const r = d.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      // A close button is anything with aria-label containing "close",
      // OR a button whose text content is "✕" / "×" / "Close" / "Cancel".
      const candidates = Array.from(d.querySelectorAll<HTMLElement>("button"));
      let hasClose = false;
      for (const b of candidates) {
        const al = (b.getAttribute("aria-label") || "").toLowerCase();
        const tx = (b.textContent || "").trim();
        if (al.includes("close") || al.includes("dismiss") || /^(✕|×|x|close|cancel)$/i.test(tx)) {
          const br = b.getBoundingClientRect();
          if (br.width >= 32 && br.height >= 32) {
            hasClose = true;
            break;
          }
        }
      }
      issues.push({ hasClose, ariaLabel: d.getAttribute("aria-label") || "" });
    }
    return issues;
  });
  for (const i of result) {
    if (!i.hasClose) {
      report(
        "MAJOR",
        ctx.viewport.key,
        ctx.route.path,
        "modal-dismiss",
        `dialog "${i.ariaLabel || "<no aria-label>"}" has no visible close affordance ≥32x32px`,
      );
    }
  }
}

// 8. Table → cards / scrollable below 640px. Any <table> on a phone
//    viewport (<640px) should either (a) be wrapped in an
//    overflow-x:auto container, OR (b) be hidden in favour of a card
//    layout (hidden md:table pattern). Native tables that just
//    overflow the viewport are the worst mobile pattern.
async function checkTableResponsiveness(ctx: CheckCtx): Promise<void> {
  if (ctx.viewport.width >= 640) return;
  const result = await ctx.page.evaluate(() => {
    const tables = Array.from(document.querySelectorAll<HTMLElement>("table"));
    const offenders: { tag: string; cls: string; w: number; vw: number }[] = [];
    for (const t of tables) {
      const cs = window.getComputedStyle(t);
      if (cs.display === "none" || cs.visibility === "hidden") continue;
      const r = t.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      // Is the table itself wider than its containing block?
      const parent = t.parentElement;
      if (!parent) continue;
      const pcs = window.getComputedStyle(parent);
      const parentScrolls = pcs.overflowX === "auto" || pcs.overflowX === "scroll";
      if (parentScrolls) continue;
      // No scrolling parent — flag if table is wider than viewport.
      if (r.width > window.innerWidth + 1) {
        offenders.push({
          tag: t.tagName.toLowerCase(),
          cls: (t.className || "").toString().slice(0, 60),
          w: Math.round(r.width),
          vw: window.innerWidth,
        });
      }
    }
    return offenders;
  });
  for (const o of result) {
    report(
      "MAJOR",
      ctx.viewport.key,
      ctx.route.path,
      "table-overflow",
      `<table class="${o.cls}"> width=${o.w}px > viewport=${o.vw}px and parent doesn't scroll`,
    );
  }
}

// 9. Sticky toasts don't overlap the bottom nav / submit buttons.
//    Sonner toasts live at the bottom by default; a toast that hides
//    the primary action is a bad pattern. Smoke doesn't trigger any
//    toasts so this is a no-op until a follow-up cycle wires
//    toast-trigger fixtures.
//    (Reserved — implemented as a placeholder so the report header
//    enumerates it.)

// ---------- per-route runner ----------
async function auditRoute(page: Page, viewport: ViewportProfile, route: RouteSpec, expandedUrl: string): Promise<void> {
  const url = `${BASE_URL}${expandedUrl}`;
  try {
    const resp = await page.goto(url, { waitUntil: "networkidle", timeout: 20_000 });
    if (resp && resp.status() >= 500) {
      report(
        "MAJOR",
        viewport.key,
        route.path,
        "http",
        `HTTP ${resp.status()} from server`,
      );
      return;
    }
    // Give the SPA a moment to hydrate beyond networkidle (React
    // microtasks for state-derived layout).
    await page.waitForSelector("body", { timeout: 5_000 }).catch(() => {});
    await page.waitForTimeout(300);
    // Screenshot first — the report links each finding to a visual.
    const shotDir = join(SHOT_ROOT, viewport.key);
    mkdirSync(shotDir, { recursive: true });
    await page.screenshot({ path: join(shotDir, `${route.key}.png`), fullPage: true }).catch(() => {});

    const ctx: CheckCtx = { page, viewport, route, url: expandedUrl };
    await checkNoHorizontalScroll(ctx);
    await checkTapTargets(ctx);
    await checkHeaderOverlap(ctx);
    await checkPrimaryNavReachable(ctx);
    await checkFormsLabeled(ctx);
    await checkModalDismissible(ctx);
    await checkTableResponsiveness(ctx);
    checkConsoleErrors(ctx);
    passLine(`${viewport.key} ${route.path}`);
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    report("MAJOR", viewport.key, route.path, "load-failure", msg);
  }
}

// ---------- markdown report writer ----------
function writeReport(target: string): void {
  // Aggregate: counts per viewport per severity.
  type Tally = { major: number; minor: number; pass: number };
  const perViewport = new Map<string, Tally>();
  for (const v of VIEWPORTS) perViewport.set(v.key, { major: 0, minor: 0, pass: 0 });
  // Count routes that produced any finding per viewport.
  const findingRoutes = new Map<string, Set<string>>();
  for (const v of VIEWPORTS) findingRoutes.set(v.key, new Set());
  for (const f of findings) {
    const t = perViewport.get(f.viewport);
    if (t) {
      if (f.severity === "MAJOR") t.major++;
      else t.minor++;
    }
    findingRoutes.get(f.viewport)?.add(f.route);
  }
  // PASS count: routes audited on that viewport minus routes with any finding.
  // Total routes audited per viewport == ROUTES.length (some may have been
  // skipped due to missing region/bucket; subtract those too).
  const totalRoutes = ROUTES.length;
  for (const v of VIEWPORTS) {
    const t = perViewport.get(v.key)!;
    t.pass = totalRoutes - (findingRoutes.get(v.key)?.size ?? 0);
  }

  const lines: string[] = [];
  lines.push(`# Mobile UI audit — ${TODAY}`);
  lines.push("");
  lines.push(`Generated by \`scripts/mobile-audit.ts\` (cycle v1.11.0.17). Target: \`${BASE_URL}\`.`);
  lines.push("");
  lines.push("READ-ONLY audit. No mutations against the deployment.");
  lines.push("");
  lines.push("## Summary");
  lines.push("");
  lines.push("| Viewport | PASS | MINOR | MAJOR |");
  lines.push("| --- | ---: | ---: | ---: |");
  for (const v of VIEWPORTS) {
    const t = perViewport.get(v.key)!;
    lines.push(`| ${v.label} | ${t.pass} | ${t.minor} | ${t.major} |`);
  }
  lines.push("");
  lines.push(`Total routes audited per viewport: ${totalRoutes}.`);
  lines.push("");
  lines.push("## Checks performed");
  lines.push("");
  lines.push("1. **horizontal-scroll** — page width must not exceed viewport width by > 1px.");
  lines.push("2. **tap-target** — every visible interactive element (a, button, input, select, textarea, role=button/link/menuitem) ≥ 44×44px on phone viewports (< 700px wide). Skipped on tablet.");
  lines.push("3. **header-overlap** — sticky top header (h-16) must not occlude the first `<h1>`.");
  lines.push("4. **nav-overflow / nav-empty** — `<nav aria-label=\"Primary\">` either fits the viewport or scrolls horizontally; at least one link visible.");
  lines.push("5. **form-label** — every visible form input on `/new`, `/edit`, or `/login` routes resolves a label (wrap, for=id, aria-label, or aria-labelledby).");
  lines.push("6. **modal-dismiss** — any open `[role=\"dialog\"]` has a visible close button ≥ 32×32px.");
  lines.push("7. **table-overflow** — `<table>` on phone viewports either has a scrolling parent or fits the viewport (no naked overflow).");
  lines.push("8. **console-error** — page should produce zero console errors on load (excludes \"Failed to load resource\" noise).");
  lines.push("");
  lines.push(`Screenshots: \`${SHOT_ROOT}/{viewport}/{route}.png\`.`);
  lines.push("");
  // Per-viewport per-route findings.
  lines.push("## Findings");
  lines.push("");
  if (findings.length === 0) {
    lines.push("No findings — every audited route passed every check on every viewport.");
    lines.push("");
  } else {
    for (const v of VIEWPORTS) {
      const vFindings = findings.filter((f) => f.viewport === v.key);
      lines.push(`### ${v.label}`);
      lines.push("");
      if (vFindings.length === 0) {
        lines.push("No findings.");
        lines.push("");
        continue;
      }
      // Group by route.
      const byRoute = new Map<string, Finding[]>();
      for (const f of vFindings) {
        const arr = byRoute.get(f.route) ?? [];
        arr.push(f);
        byRoute.set(f.route, arr);
      }
      for (const [route, items] of byRoute) {
        const maxSev = items.some((i) => i.severity === "MAJOR") ? "MAJOR" : "MINOR";
        lines.push(`- **${route}** [${maxSev}]`);
        for (const f of items) {
          lines.push(`  - \`${f.category}\`: ${f.detail}`);
        }
      }
      lines.push("");
    }
  }
  writeFileSync(target, lines.join("\n"), "utf8");
}

// ---------- main ----------
async function main(): Promise<number> {
  info("basement mobile UI audit");
  info(`target:      ${BASE_URL}`);
  info(`user:        ${USERNAME}`);
  info(`viewports:   ${VIEWPORTS.map((v) => v.label).join(", ")}`);
  info(`routes:      ${ROUTES.length}`);
  info(`screenshots: ${SHOT_ROOT}`);
  info(`report:      ${REPORT_PATH}`);

  let browser: Browser | undefined;
  try {
    browser = await chromium.launch({ headless: true });

    // Discover real region/bucket/cluster IDs once via a desktop context.
    // We need these to expand the {regionId}/{bid}/{cid} placeholder
    // routes. READ-ONLY GETs — no mutation.
    let realRegionId = "";
    let realBucketId = "";
    let realClusterId = "";
    {
      const discoverCtx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        await loginViaApi(discoverCtx);
        const page = await discoverCtx.newPage();
        await elevateToAdmin(page).catch(() => {});
        const r = await page.request.get(`${BASE_URL}/api/v1/user/regions`);
        if (r.ok()) {
          const arr = await r.json();
          if (Array.isArray(arr) && arr.length > 0) realRegionId = arr[0].id;
        }
        if (realRegionId) {
          const b = await page.request.get(`${BASE_URL}/api/v1/user/regions/${realRegionId}/buckets`);
          if (b.ok()) {
            const body = await b.json();
            const buckets = Array.isArray(body) ? body : body?.buckets ?? [];
            if (buckets.length > 0) realBucketId = buckets[0].id;
          }
        }
        const c = await page.request.get(`${BASE_URL}/api/v1/admin/clusters`);
        if (c.ok()) {
          const arr = await c.json();
          if (Array.isArray(arr) && arr.length > 0) realClusterId = arr[0].id;
        }
      } finally {
        await discoverCtx.close();
      }
    }
    info(`  discovered: region=${realRegionId || "<none>"} bucket=${realBucketId || "<none>"} cluster=${realClusterId || "<none>"}`);

    function expand(p: string): string | null {
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

    for (const v of VIEWPORTS) {
      section(`=== ${v.label} ===`);
      const ctx = await browser.newContext({
        viewport: { width: v.width, height: v.height },
        deviceScaleFactor: 2,
        isMobile: v.isMobile,
        hasTouch: v.hasTouch,
        ignoreHTTPSErrors: false,
      });
      try {
        await loginViaApi(ctx);
        const page = await ctx.newPage();
        attachListeners(page, v.key);
        await elevateToAdmin(page).catch(() => {});
        for (const route of ROUTES) {
          const expanded = expand(route.path);
          if (expanded === null) {
            skipLine(`${v.key} ${route.path}`, "required param not discoverable");
            continue;
          }
          if (route.requiresAdmin) {
            // Re-elevate before admin nav — same pattern as comprehensive-smoke.
            await elevateToAdmin(page).catch(() => {});
            await page.request.get(`${BASE_URL}/api/v1/auth/me`).catch(() => {});
          }
          await auditRoute(page, v, route, expanded);
        }
      } finally {
        await ctx.close();
      }
    }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    process.stderr.write(`[FAIL] audit harness error: ${msg}\n`);
    return 1;
  } finally {
    if (browser) await browser.close().catch(() => {});
  }

  // Write the markdown report regardless of findings.
  try {
    writeReport(REPORT_PATH);
    info(`\nreport written: ${REPORT_PATH}`);
  } catch (e) {
    warnLine(`failed to write report: ${e instanceof Error ? e.message : String(e)}`);
  }

  // Summary line.
  const major = findings.filter((f) => f.severity === "MAJOR").length;
  const minor = findings.filter((f) => f.severity === "MINOR").length;
  process.stdout.write(
    `\n${C.bold}Summary${C.reset}: ${C.red}${major} MAJOR${C.reset}, ${C.yellow}${minor} MINOR${C.reset} across ${VIEWPORTS.length} viewports × ${ROUTES.length} routes.\n`,
  );
  return 0;
}

const code = await main();
process.exit(code);
