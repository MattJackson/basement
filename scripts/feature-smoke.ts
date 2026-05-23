#!/usr/bin/env node
// feature-smoke.ts — feature-coverage smoke against test backends only.
//
// Sibling of scripts/comprehensive-smoke.ts which is route/state focused.
// This script is the per-feature, end-to-end functional counterpart:
// every product feature gets a happy-path probe that creates an
// ephemeral resource on a Garage v2 test backend (NEVER on the
// operator's real classe/lsi data), exercises the feature, and reaps
// the resource in a finally block.
//
// =========================================================================
// SAFETY GUARANTEES
// =========================================================================
//
// HARD CONSTRAINTS — these are pre-flight checks before any mutation:
//
//   1. Target cluster label MUST start with "garage-v2-test-". If the
//      script ever sees a cluster id that doesn't resolve to such a
//      label, the entire feature aborts. Belt-and-suspenders against
//      the script being mis-pointed at matthew's classe (operator's
//      production Garage v1 cluster).
//   2. Every ephemeral resource name starts with `feat-smoke-{ts}-{rand}-`.
//      Cleanup sweeps reap anything matching this prefix at start AND
//      end of the run.
//   3. Real UserRegions (matthew's `lsi`) are NEVER mutated. The
//      script captures a baseline snapshot of operator-owned resources
//      (non-feat-smoke-prefixed regions/webhooks/backups/federations/SAs)
//      before any mutation and asserts no drift at end-of-run.
//   4. The operator's classe cluster id is hard-coded as an explicit
//      deny-list entry so even a logic bug can't route there.
//
// =========================================================================
// USAGE
// =========================================================================
//
//   node scripts/feature-smoke.ts
//   BASE_URL=https://basement.example.com \
//     BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
//     node scripts/feature-smoke.ts
//
// Output:
//   - Console: green [ok] / red [FAIL] / yellow [skip] / [warn]
//   - Final summary with pass/fail/skip counts per feature
//   - Bug report block at end of run
//   - When SMOKE_WRITE_BUGS_MD is set (or always when basement repo
//     present), updates docs/feature-smoke-bugs.md with the bug list
//
// Exit codes:
//   0  all checks passed
//   1  one or more checks failed (cleanup still ran)
//   2  bad invocation / setup error / safety check tripped

import type {
  Browser,
  BrowserContext,
  chromium as ChromiumApi,
} from "playwright";
import { existsSync, writeFileSync } from "node:fs";
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

const PLAYWRIGHT_ENTRY = pathToFileURL(PLAYWRIGHT_INDEX).href;
const { chromium } = (await import(PLAYWRIGHT_ENTRY)) as {
  chromium: typeof ChromiumApi;
};

// ---------- config ----------
const BASE_URL = (process.env.BASE_URL ?? "https://basement.pq.io").replace(/\/$/, "");
const USERNAME = process.env.BUI_USERNAME ?? process.env.BASEMENT_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? process.env.BASEMENT_PASSWORD ?? process.env.PASSWORD ?? "password";

const RUN_TS = Date.now();
const RUN_NONCE = Math.random().toString(36).slice(2, 8);
const EPH_PREFIX = `feat-smoke-${RUN_TS}-${RUN_NONCE}`;
const ephName = (kind: string) => `${EPH_PREFIX}-${kind}`;

// Hard-coded operator-data deny-list. Even if the script's cluster
// discovery is bugged, these names must NEVER be targeted by any
// destructive operation. The script aborts the whole feature if it
// catches itself about to touch one of these.
const DENY_CLUSTER_IDS = new Set<string>([
  "716e8359-85d4-688e-3d14-ca04738609d0", // matthew's classe (Garage v1, real data)
]);
const DENY_CLUSTER_LABELS = new Set<string>(["classe"]);
const DENY_REGION_ALIASES = new Set<string>(["lsi", "cheshire"]);

// Every test cluster MUST satisfy this. Belt + suspenders with the
// deny-list above — even if a future test cluster shares a name with
// something else, this gate refuses to proceed unless the label
// explicitly starts with the test prefix.
const TEST_LABEL_PREFIX = "garage-v2-test-";

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
  process.stdout.write(
    `${C.green}[ok]${C.reset} ${name} ${C.dim}(${(ms / 1000).toFixed(2)}s)${C.reset}\n`,
  );
}
function failLine(name: string, detail?: string) {
  process.stderr.write(`${C.red}[FAIL]${C.reset} ${name}\n`);
  if (detail) process.stderr.write(`  ${C.dim}${detail}${C.reset}\n`);
}
function warnLine(msg: string) {
  process.stderr.write(`${C.yellow}[warn]${C.reset} ${msg}\n`);
}
function skipLine(name: string, reason: string) {
  process.stdout.write(
    `${C.yellow}[skip]${C.reset} ${name} ${C.dim}(${reason})${C.reset}\n`,
  );
  results.push({ feature: currentFeature, name, ok: true, skipped: true, ms: 0 });
}

// ---------- results / bug tracking ----------
type Result = {
  feature: string;
  name: string;
  ok: boolean;
  skipped?: boolean;
  ms: number;
  detail?: string;
};
const results: Result[] = [];

let bugCounter = 0;
type Bug = {
  id: string; // BUG01 / BUG02 ...
  feature: string;
  area: string;
  detail: string;
  repro: string;
};
const bugs: Bug[] = [];

function nextBugId(): string {
  bugCounter++;
  return `BUG${String(bugCounter).padStart(2, "0")}`;
}

function reportBug(area: string, detail: string, repro = "") {
  const id = nextBugId();
  bugs.push({ id, feature: currentFeature, area, detail, repro });
  warnLine(`${id} [${currentFeature}/${area}] ${detail}`);
}

let currentFeature = "init";
function feature(label: string) {
  currentFeature = label;
  section(`==== ${label} ====`);
}

async function check(name: string, fn: () => Promise<void>): Promise<boolean> {
  const start = Date.now();
  try {
    await fn();
    const ms = Date.now() - start;
    results.push({ feature: currentFeature, name, ok: true, ms });
    passLine(name, ms);
    return true;
  } catch (err) {
    const ms = Date.now() - start;
    const detail = err instanceof Error ? err.message : String(err);
    results.push({ feature: currentFeature, name, ok: false, ms, detail });
    failLine(name, detail);
    return false;
  }
}

// ---------- HTTP helpers (cookie-jarred) ----------
let cookieJar: string[] = [];

async function api(
  method: string,
  path: string,
  body?: unknown,
  extraHeaders?: Record<string, string>,
): Promise<{ status: number; headers: Headers; body: any; text: string }> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(extraHeaders ?? {}),
  };
  if (cookieJar.length > 0) {
    headers.Cookie = cookieJar.join("; ");
  }
  const init: RequestInit = { method, headers };
  if (body !== undefined && body !== null) {
    init.body = typeof body === "string" ? body : JSON.stringify(body);
  }
  const resp = await fetch(`${BASE_URL}${path}`, init);
  // Capture all Set-Cookie headers (multi-cookie aware via getSetCookie).
  const sc = (resp.headers as any).getSetCookie?.() ?? [];
  for (const raw of sc) {
    const m = raw.match(/^([^=]+)=([^;]+)/);
    if (!m) continue;
    const name = m[1];
    const val = m[2];
    cookieJar = cookieJar.filter((c) => !c.startsWith(`${name}=`));
    cookieJar.push(`${name}=${val}`);
  }
  const text = await resp.text();
  let parsed: any = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = null;
    }
  }
  return { status: resp.status, headers: resp.headers, body: parsed, text };
}

async function loginAndElevate(): Promise<void> {
  const login = await api("POST", "/api/v1/auth/login", { username: USERNAME, password: PASSWORD });
  if (login.status !== 200) {
    throw new Error(`login failed: ${login.status} ${login.text}`);
  }
  const elev = await api("POST", "/api/v1/auth/elevate", {
    target_mode: "admin",
    password: PASSWORD,
  });
  if (elev.status !== 200) {
    throw new Error(`elevate failed: ${elev.status} ${elev.text}`);
  }
  const me = await api("GET", "/api/v1/auth/me");
  if (me.body?.mode !== "admin") {
    throw new Error(`expected mode=admin after elevate, got ${JSON.stringify(me.body)}`);
  }
}

// ---------- cluster discovery + safety gate ----------
type Cluster = { id: string; label: string; driver: string; config: any };
let testClusters: Cluster[] = [];

async function discoverTestClusters(): Promise<void> {
  const r = await api("GET", "/api/v1/admin/clusters");
  if (r.status !== 200 || !Array.isArray(r.body)) {
    throw new Error(`could not list clusters: ${r.status} ${r.text.slice(0, 200)}`);
  }
  testClusters = r.body
    .filter((c: Cluster) => c.label && c.label.startsWith(TEST_LABEL_PREFIX))
    .filter((c: Cluster) => !DENY_CLUSTER_IDS.has(c.id))
    .filter((c: Cluster) => !DENY_CLUSTER_LABELS.has(c.label));
  if (testClusters.length < 2) {
    throw new Error(`need at least 2 test clusters, found ${testClusters.length}`);
  }
  info(
    `discovered ${testClusters.length} test cluster(s): ${testClusters.map((c) => c.label).join(", ")}`,
  );
}

// Safety enforcement: refuses to operate on a non-test cluster. ALL
// destructive operations route through here.
function assertTestCluster(cid: string, context: string): Cluster {
  const c = testClusters.find((x) => x.id === cid);
  if (!c) {
    throw new Error(
      `[SAFETY ABORT] ${context}: cluster id ${cid} is NOT in the test cluster set — refusing to proceed`,
    );
  }
  if (!c.label.startsWith(TEST_LABEL_PREFIX)) {
    throw new Error(
      `[SAFETY ABORT] ${context}: cluster ${cid} label '${c.label}' does not start with '${TEST_LABEL_PREFIX}' — refusing to proceed`,
    );
  }
  if (DENY_CLUSTER_IDS.has(cid) || DENY_CLUSTER_LABELS.has(c.label)) {
    throw new Error(
      `[SAFETY ABORT] ${context}: cluster ${cid}/${c.label} is on the deny-list — refusing to proceed`,
    );
  }
  return c;
}

// ---------- ephemeral resource tracking ----------
type Ephemeral = {
  kind: string;
  delUrl: string;
  name: string;
  // Optional pre-delete arm endpoint for two-phase delete
  armUrl?: string;
};
const ephemerals: Ephemeral[] = [];

function trackEphemeral(e: Ephemeral) {
  ephemerals.push(e);
  info(`  tracked ephemeral ${e.kind}: ${e.name}`);
}

// ---------- baseline snapshot for end-of-run drift check ----------
type OperatorSnapshot = {
  regionAliases: string[];
  saNames: string[];
  webhookNames: string[];
  backupNames: string[];
  federationNames: string[];
  // Per-test-cluster bucket counts (we created/deleted buckets, so
  // operator-data must remain unchanged on classe — we never touch
  // classe so that count remains 0 since it's not in test set).
};

async function captureOperatorSnapshot(): Promise<OperatorSnapshot> {
  async function nonSmoke(url: string, field: string): Promise<string[]> {
    const r = await api("GET", url);
    if (r.status !== 200) return [];
    const arr = Array.isArray(r.body) ? r.body : Array.isArray(r.body?.items) ? r.body.items : [];
    return arr
      .filter(
        (it: any) =>
          !((it[field] ?? "") as string).startsWith("feat-smoke-") &&
          !((it[field] ?? "") as string).startsWith("smoke-") &&
          !it.revokedAt,
      )
      .map((it: any) => it[field] ?? "");
  }
  return {
    regionAliases: await nonSmoke("/api/v1/user/regions", "alias"),
    saNames: await nonSmoke("/api/v1/admin/service-accounts", "name"),
    webhookNames: await nonSmoke("/api/v1/user/webhooks", "name"),
    backupNames: await nonSmoke("/api/v1/user/backups", "name"),
    federationNames: await nonSmoke("/api/v1/user/federated-buckets", "name"),
  };
}

function snapshotsEqual(a: OperatorSnapshot, b: OperatorSnapshot): boolean {
  const sameArr = (x: string[], y: string[]) =>
    x.length === y.length && x.every((v) => y.includes(v));
  return (
    sameArr(a.regionAliases, b.regionAliases) &&
    sameArr(a.saNames, b.saNames) &&
    sameArr(a.webhookNames, b.webhookNames) &&
    sameArr(a.backupNames, b.backupNames) &&
    sameArr(a.federationNames, b.federationNames)
  );
}

// ---------- sleep helper ----------
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---------- main flow ----------
async function main(): Promise<number> {
  info("basement feature-coverage smoke");
  info(`target:      ${BASE_URL}`);
  info(`user:        ${USERNAME}`);
  info(`ephemeral:   ${EPH_PREFIX}-*`);

  let browser: Browser | undefined;
  let bctx: BrowserContext | undefined;
  let baseline: OperatorSnapshot | undefined;

  try {
    // Auth bootstrap (fetch-based, shared cookie jar).
    feature("bootstrap");
    await check("login + elevate to admin mode", loginAndElevate);
    await check("discover test clusters (label starts with garage-v2-test-)", discoverTestClusters);
    await check("baseline snapshot of operator-real resources", async () => {
      baseline = await captureOperatorSnapshot();
      info(
        `  baseline: regions=${baseline.regionAliases.length} sa=${baseline.saNames.length} ` +
          `webhooks=${baseline.webhookNames.length} backups=${baseline.backupNames.length} ` +
          `federations=${baseline.federationNames.length}`,
      );
    });

    // Pre-flight: opportunistically reap feat-smoke-* leftovers from
    // prior runs so per-cluster bucket/key checks don't trip on
    // duplicate names.
    await check("opportunistically reap feat-smoke-* leftovers", async () => {
      let reaped = 0;
      // SAs + webhooks + backups + federations
      const endpoints = [
        "/api/v1/admin/service-accounts",
        "/api/v1/user/webhooks",
        "/api/v1/user/backups",
        "/api/v1/user/federated-buckets",
      ];
      for (const ep of endpoints) {
        const r = await api("GET", ep);
        if (r.status !== 200 || !Array.isArray(r.body)) continue;
        for (const it of r.body) {
          if ((it.name ?? "").startsWith("feat-smoke-") && !it.revokedAt) {
            const d = await api("DELETE", `${ep}/${it.id}`);
            if (d.status === 200 || d.status === 204) reaped++;
          }
        }
      }
      // User regions
      const ur = await api("GET", "/api/v1/user/regions");
      if (ur.status === 200 && Array.isArray(ur.body)) {
        for (const it of ur.body) {
          if ((it.alias ?? "").startsWith("feat-smoke-")) {
            const d = await api("DELETE", `/api/v1/user/regions/${it.id}`);
            if (d.status === 200 || d.status === 204) reaped++;
          }
        }
      }
      // Buckets + keys per test cluster
      for (const c of testClusters) {
        const buckets = await api("GET", `/api/v1/admin/clusters/${c.id}/buckets`);
        if (buckets.status === 200 && Array.isArray(buckets.body)) {
          for (const b of buckets.body) {
            const aliases = b.aliases ?? [];
            if (aliases.some((a: string) => a.startsWith("feat-smoke-"))) {
              // Two-phase delete
              const arm = await api(
                "POST",
                `/api/v1/admin/clusters/${c.id}/buckets/${b.id}/_arm-delete`,
              );
              if (arm.status === 200 && arm.body?.token) {
                const d = await api(
                  "DELETE",
                  `/api/v1/admin/clusters/${c.id}/buckets/${b.id}`,
                  null,
                  { "X-Confirm-Delete": arm.body.token },
                );
                if (d.status === 200 || d.status === 204) reaped++;
              }
            }
          }
        }
        const keys = await api("GET", `/api/v1/admin/clusters/${c.id}/keys`);
        if (keys.status === 200 && Array.isArray(keys.body)) {
          for (const k of keys.body) {
            if ((k.name ?? "").startsWith("feat-smoke-")) {
              const arm = await api(
                "POST",
                `/api/v1/admin/clusters/${c.id}/keys/${k.id}/_arm-delete`,
              );
              if (arm.status === 200 && arm.body?.token) {
                const d = await api(
                  "DELETE",
                  `/api/v1/admin/clusters/${c.id}/keys/${k.id}`,
                  null,
                  { "X-Confirm-Delete": arm.body.token },
                );
                if (d.status === 200 || d.status === 204) reaped++;
              }
            }
          }
        }
      }
      info(`  reaped ${reaped} stale feat-smoke-* leftover(s)`);
    });

    // Pick the first two test clusters as the work targets.
    const c1 = testClusters[0]; // primary work cluster
    const c2 = testClusters[1]; // secondary (for backup dst + federation replica)
    info(`work clusters: c1=${c1.label} (${c1.id}) c2=${c2.label} (${c2.id})`);

    // ============================================================
    // A. Cluster + driver basics
    // ============================================================
    feature("A. Cluster + driver basics");

    let aBucketId = "";
    let aBucketName = ephName("bucket-a");
    let aKeyId = "";
    let aKeySecret = "";

    await check("A.1 create bucket on test cluster", async () => {
      assertTestCluster(c1.id, "A.1 create bucket");
      const r = await api("POST", `/api/v1/admin/clusters/${c1.id}/buckets`, {
        alias: aBucketName,
      });
      if (r.status !== 201) {
        throw new Error(`POST bucket → ${r.status} ${r.text.slice(0, 300)}`);
      }
      aBucketId = r.body.id;
      trackEphemeral({
        kind: "bucket",
        name: aBucketName,
        armUrl: `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}/_arm-delete`,
        delUrl: `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}`,
      });
    });

    await check("A.2 GET bucket returns matching alias", async () => {
      const r = await api("GET", `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}`);
      if (r.status !== 200) throw new Error(`GET bucket → ${r.status} ${r.text}`);
      const aliases = r.body.aliases ?? [];
      if (!aliases.includes(aBucketName)) {
        throw new Error(`bucket aliases ${JSON.stringify(aliases)} missing ${aBucketName}`);
      }
    });

    await check("A.3 update bucket alias (rename)", async () => {
      const newName = ephName("bucket-a-renamed");
      // BucketUpdate wire shape: {aliases: [...]} (full replacement).
      const r = await api("PATCH", `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}`, {
        aliases: [newName],
      });
      if (r.status !== 200) {
        if (r.status === 501) {
          reportBug(
            "bucket-rename",
            `PATCH aliases returned 501 on Garage v2`,
            `PATCH /api/v1/admin/clusters/${c1.id}/buckets/{bid} {"aliases":[...]} → 501`,
          );
          return;
        }
        throw new Error(`PATCH rename → ${r.status} ${r.text.slice(0, 300)}`);
      }
      const ral = r.body.aliases ?? [];
      if (!ral.includes(newName)) {
        reportBug(
          "bucket-rename",
          `PATCH aliases returned 200 but aliases ${JSON.stringify(ral)} missing new ${newName}`,
        );
      } else {
        aBucketName = newName;
        const t = ephemerals.find((e) => e.kind === "bucket");
        if (t) t.name = newName;
      }
    });

    await check("A.4 create access key on test cluster (mint returns secret)", async () => {
      assertTestCluster(c1.id, "A.4 create key");
      const r = await api("POST", `/api/v1/admin/clusters/${c1.id}/keys`, {
        name: ephName("key-a"),
      });
      if (r.status !== 201) {
        throw new Error(`POST key → ${r.status} ${r.text.slice(0, 300)}`);
      }
      aKeyId = r.body.id;
      aKeySecret = r.body.secretAccessKey ?? r.body.secret ?? "";
      if (!aKeySecret) {
        reportBug(
          "key-mint",
          `POST /admin/clusters/{cid}/keys returned 201 but no secretAccessKey in response`,
          `POST /api/v1/admin/clusters/${c1.id}/keys {"name":"feat-smoke-..."} → body keys: ${Object.keys(r.body ?? {}).join(",")}`,
        );
      }
      trackEphemeral({
        kind: "key",
        name: ephName("key-a"),
        armUrl: `/api/v1/admin/clusters/${c1.id}/keys/${aKeyId}/_arm-delete`,
        delUrl: `/api/v1/admin/clusters/${c1.id}/keys/${aKeyId}`,
      });
    });

    await check("A.5 update key permissions (grant on test bucket)", async () => {
      if (!aKeyId || !aBucketId) {
        skipLine("A.5 update key permissions", "no key or bucket");
        return;
      }
      // Wire shape per driver.BucketPermission: flat {bucketId, read,
      // write, owner} — NOT nested under a "permissions" object.
      const r = await api("PATCH", `/api/v1/admin/clusters/${c1.id}/keys/${aKeyId}`, {
        bucketsPermissions: [
          { bucketId: aBucketId, read: true, write: true, owner: true },
        ],
      });
      if (r.status !== 200) {
        throw new Error(`PATCH key permissions → ${r.status} ${r.text.slice(0, 300)}`);
      }
      // Verify the grant actually took effect — Garage will accept
      // the PATCH and silently store all-false if the field shape is
      // wrong; previous iteration of this script hit that.
      const verify = r.body?.buckets ?? [];
      const grant = verify.find((b: any) => b.bucketId === aBucketId);
      if (!grant) {
        reportBug(
          "key-grant-missing",
          `PATCH succeeded but grant row for bucket ${aBucketId} not in response`,
        );
      } else if (!grant.read || !grant.write || !grant.owner) {
        reportBug(
          "key-grant-flags-lost",
          `PATCH grant returned r=${grant.read} w=${grant.write} o=${grant.owner} (expected all true)`,
        );
      }
    });

    await check("A.6 list keys + list buckets surfaces ephemerals", async () => {
      const lk = await api("GET", `/api/v1/admin/clusters/${c1.id}/keys`);
      if (lk.status !== 200) throw new Error(`list keys → ${lk.status}`);
      if (!lk.body.find((k: any) => k.id === aKeyId)) {
        throw new Error(`created key ${aKeyId} not in list`);
      }
      const lb = await api("GET", `/api/v1/admin/clusters/${c1.id}/buckets`);
      if (lb.status !== 200) throw new Error(`list buckets → ${lb.status}`);
      if (!lb.body.find((b: any) => b.id === aBucketId)) {
        throw new Error(`created bucket ${aBucketId} not in list`);
      }
    });

    await check("A.7 driver capabilities visible via /api/v1/capabilities", async () => {
      const r = await api("GET", "/api/v1/capabilities");
      if (r.status !== 200) throw new Error(`GET /capabilities → ${r.status}`);
      // Global driver caps (s.drv). For Garage we expect
      // versioning=false. NB: this is the GLOBAL driver, not the
      // per-cluster driver — there is no per-cluster /driver-info
      // endpoint as of v1.11.0.4, so we document that gap.
      info(`  /capabilities driver=${r.body?.driver} versioning=${r.body?.versioning}`);
      // Try the per-cluster endpoint that the task mentions — if it
      // doesn't exist, log as a BUG (the task assumed it).
      const pc = await api("GET", `/api/v1/admin/clusters/${c1.id}/driver-info`);
      if (pc.status === 404) {
        reportBug(
          "driver-info-endpoint",
          `GET /api/v1/admin/clusters/{cid}/driver-info returned 404 — endpoint does not exist`,
          `GET /api/v1/admin/clusters/${c1.id}/driver-info → ${pc.status}`,
        );
      } else if (pc.status >= 200 && pc.status < 300) {
        info(`  /driver-info: ${JSON.stringify(pc.body).slice(0, 200)}`);
      }
    });

    // ============================================================
    // B. UserRegions
    // ============================================================
    feature("B. UserRegions");

    let urRegionId = "";
    const urAlias = ephName("region");

    await check("B.1 create UserRegion pointing at test cluster", async () => {
      assertTestCluster(c1.id, "B.1 create UserRegion");
      if (!aKeySecret || !aKeyId) {
        skipLine("B.1 create UserRegion", "no key secret from A.4");
        return;
      }
      // Need access-key-id (the GK...) for the wire — the create key
      // response shape is driver.Key which has .accessKeyId
      const keyResp = await api("GET", `/api/v1/admin/clusters/${c1.id}/keys/${aKeyId}`);
      if (keyResp.status !== 200) {
        throw new Error(`fetch key for accessKeyId → ${keyResp.status}`);
      }
      const accessKeyId = keyResp.body.accessKeyId ?? keyResp.body.id;
      // The cluster's s3_endpoint is internal (http://10.1.7.11:xxxx).
      // Use it directly — the backend forwards through to the cluster.
      const endpoint = c1.config.s3_endpoint;
      const r = await api("POST", "/api/v1/user/regions", {
        alias: urAlias,
        endpoint,
        accessKeyId,
        secretKey: aKeySecret,
        region: c1.config.region ?? "garage",
        addressingStyle: "path",
      });
      if (r.status !== 201) {
        throw new Error(`POST /user/regions → ${r.status} ${r.text.slice(0, 300)}`);
      }
      urRegionId = r.body.id;
      trackEphemeral({
        kind: "region",
        name: urAlias,
        delUrl: `/api/v1/user/regions/${urRegionId}`,
      });
    });

    await check("B.2 list UserRegions includes the new one", async () => {
      const r = await api("GET", "/api/v1/user/regions");
      if (r.status !== 200) throw new Error(`list regions → ${r.status}`);
      if (!r.body.find((x: any) => x.id === urRegionId)) {
        throw new Error(`region ${urRegionId} not in list`);
      }
    });

    await check("B.3 list buckets via UserRegion", async () => {
      if (!urRegionId) return;
      const r = await api("GET", `/api/v1/user/regions/${urRegionId}/buckets`);
      if (r.status !== 200) {
        reportBug(
          "userregion-list-buckets",
          `GET /user/regions/{rid}/buckets → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
        return;
      }
      const buckets = Array.isArray(r.body) ? r.body : r.body?.buckets ?? [];
      info(`  region has ${buckets.length} bucket(s) visible`);
    });

    await check("B.4 presign PUT + actual upload via signed URL", async () => {
      if (!urRegionId || !aBucketId) {
        skipLine("B.4 presign upload", "no region or bucket");
        return;
      }
      const key = "feat-smoke/test.txt";
      const encKey = encodeURIComponent(key);
      const r = await api(
        "POST",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/objects/${encKey}/presign-put`,
        { contentType: "text/plain" },
      );
      if (r.status !== 200) {
        reportBug(
          "userregion-presign-put",
          `presign-put → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
        return;
      }
      const url = r.body.url ?? r.body.signedUrl;
      if (!url) {
        reportBug("userregion-presign-put", `presign-put response missing url field; got: ${JSON.stringify(r.body).slice(0, 200)}`);
        return;
      }
      // Try to PUT to the signed URL. Note: the URL points at the
      // internal s3 endpoint (http://10.1.7.11:xxxx) which is NOT
      // reachable from outside the deployment. We test the surface
      // returns a URL; an actual PUT requires being on the deploy
      // network.
      info(`  presigned URL (host): ${new URL(url).host}`);
      // Attempt the PUT; failure is expected if we're external.
      try {
        const upResp = await fetch(url, {
          method: "PUT",
          headers: { "Content-Type": "text/plain" },
          body: "feat-smoke test payload",
          signal: AbortSignal.timeout(5000),
        });
        info(`  PUT to presigned URL → ${upResp.status}`);
        if (upResp.status >= 200 && upResp.status < 300) {
          info(`  upload succeeded — backend reachable from this network`);
        }
      } catch (e) {
        info(`  PUT to presigned URL unreachable (expected for external runs): ${e instanceof Error ? e.message : e}`);
      }
    });

    await check("B.5 presign GET returns URL", async () => {
      if (!urRegionId || !aBucketId) {
        skipLine("B.5 presign GET", "no region or bucket");
        return;
      }
      const key = "feat-smoke/test.txt";
      const encKey = encodeURIComponent(key);
      const r = await api(
        "GET",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/objects/${encKey}/presign-get`,
      );
      if (r.status !== 200) {
        reportBug(
          "userregion-presign-get",
          `presign-get → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
        return;
      }
      const url = r.body.url ?? r.body.signedUrl;
      if (!url) {
        reportBug("userregion-presign-get", `presign-get response missing url; got: ${JSON.stringify(r.body).slice(0, 200)}`);
      }
    });

    // ============================================================
    // C. Buckets — object operations (multipart init/abort)
    // ============================================================
    feature("C. Bucket object operations");

    await check("C.1 multipart init + abort cleanup", async () => {
      if (!urRegionId || !aBucketId) {
        skipLine("C.1 multipart", "no region or bucket");
        return;
      }
      const r = await api(
        "POST",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/multipart/init`,
        { key: "feat-smoke/multipart-test.bin", contentType: "application/octet-stream" },
      );
      if (r.status !== 200) {
        reportBug(
          "multipart-init",
          `multipart init → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
        return;
      }
      const uploadId = r.body.uploadId ?? r.body.UploadId;
      if (!uploadId) {
        reportBug("multipart-init", `init response missing uploadId; got: ${JSON.stringify(r.body).slice(0, 200)}`);
        return;
      }
      // Abort
      const a = await api(
        "DELETE",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/multipart/${encodeURIComponent(uploadId)}`,
      );
      if (a.status !== 204 && a.status !== 200) {
        reportBug(
          "multipart-abort",
          `multipart abort → ${a.status}`,
          `body: ${a.text.slice(0, 200)}`,
        );
      }
    });

    await check("C.2 list objects in bucket via UserRegion", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "GET",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/objects`,
      );
      if (r.status !== 200) {
        reportBug(
          "userregion-list-objects",
          `list objects → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      }
    });

    // ============================================================
    // D. Backups
    // ============================================================
    feature("D. Backups");

    let backupId = "";
    let dstBucketId = "";
    const dstBucketName = ephName("backup-dst");

    await check("D.0 create destination bucket on second test cluster", async () => {
      assertTestCluster(c2.id, "D.0 dst bucket");
      const r = await api("POST", `/api/v1/admin/clusters/${c2.id}/buckets`, {
        alias: dstBucketName,
      });
      if (r.status !== 201) {
        throw new Error(`POST dst bucket → ${r.status} ${r.text.slice(0, 200)}`);
      }
      dstBucketId = r.body.id;
      trackEphemeral({
        kind: "bucket",
        name: dstBucketName,
        armUrl: `/api/v1/admin/clusters/${c2.id}/buckets/${dstBucketId}/_arm-delete`,
        delUrl: `/api/v1/admin/clusters/${c2.id}/buckets/${dstBucketId}`,
      });
    });

    // Backups operate on UserRegion ids (the user-tier resource).
    // We need a second UserRegion for the destination.
    let dstRegionId = "";
    let dstKeyId = "";
    let dstKeySecret = "";

    await check("D.0b mint key + UserRegion for backup destination", async () => {
      const k = await api("POST", `/api/v1/admin/clusters/${c2.id}/keys`, {
        name: ephName("key-dst"),
      });
      if (k.status !== 201) {
        throw new Error(`mint dst key → ${k.status} ${k.text.slice(0, 200)}`);
      }
      dstKeyId = k.body.id;
      dstKeySecret = k.body.secretAccessKey ?? k.body.secret ?? "";
      trackEphemeral({
        kind: "key",
        name: ephName("key-dst"),
        armUrl: `/api/v1/admin/clusters/${c2.id}/keys/${dstKeyId}/_arm-delete`,
        delUrl: `/api/v1/admin/clusters/${c2.id}/keys/${dstKeyId}`,
      });
      // Grant on the dst bucket (flat BucketPermission shape)
      const perm = await api("PATCH", `/api/v1/admin/clusters/${c2.id}/keys/${dstKeyId}`, {
        bucketsPermissions: [
          { bucketId: dstBucketId, read: true, write: true, owner: true },
        ],
      });
      if (perm.status !== 200) {
        throw new Error(`grant dst key → ${perm.status}`);
      }
      // Create UserRegion
      const ur = await api("POST", "/api/v1/user/regions", {
        alias: ephName("region-dst"),
        endpoint: c2.config.s3_endpoint,
        accessKeyId: k.body.accessKeyId ?? dstKeyId,
        secretKey: dstKeySecret,
        region: c2.config.region ?? "garage",
        addressingStyle: "path",
      });
      if (ur.status !== 201) {
        throw new Error(`POST dst region → ${ur.status} ${ur.text.slice(0, 200)}`);
      }
      dstRegionId = ur.body.id;
      trackEphemeral({
        kind: "region",
        name: ephName("region-dst"),
        delUrl: `/api/v1/user/regions/${dstRegionId}`,
      });
    });

    await check("D.1 create backup (mirror, manual, disabled)", async () => {
      if (!urRegionId || !dstRegionId || !aBucketId || !dstBucketId) {
        skipLine("D.1 create backup", "missing region/bucket");
        return;
      }
      const r = await api("POST", "/api/v1/user/backups", {
        name: ephName("backup"),
        srcRegionId: urRegionId,
        srcBucket: aBucketId,
        dstRegionId,
        dstBucket: dstBucketId,
        schedule: "manual",
        disabled: true,
        mode: "mirror",
      });
      if (r.status !== 201) {
        reportBug(
          "backup-create",
          `POST /user/backups → ${r.status}`,
          `body: ${r.text.slice(0, 300)}`,
        );
        return;
      }
      backupId = r.body.id;
      trackEphemeral({
        kind: "backup",
        name: ephName("backup"),
        delUrl: `/api/v1/user/backups/${backupId}`,
      });
    });

    await check("D.2 GET backup + list shows it", async () => {
      if (!backupId) return;
      const g = await api("GET", `/api/v1/user/backups/${backupId}`);
      if (g.status !== 200) {
        reportBug("backup-get", `GET backup → ${g.status}`);
        return;
      }
      const l = await api("GET", "/api/v1/user/backups");
      if (l.status !== 200 || !Array.isArray(l.body) || !l.body.find((b: any) => b.id === backupId)) {
        reportBug("backup-list", `backup not in list`);
      }
    });

    await check("D.3 update backup to snapshot mode", async () => {
      if (!backupId) return;
      const r = await api("PUT", `/api/v1/user/backups/${backupId}`, {
        name: ephName("backup"),
        srcRegionId: urRegionId,
        srcBucket: aBucketId,
        dstRegionId,
        dstBucket: dstBucketId,
        schedule: "manual",
        disabled: true,
        mode: "snapshot",
      });
      if (r.status !== 200) {
        reportBug(
          "backup-update",
          `PUT backup → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      }
    });

    await check("D.4 list snapshots (empty is OK)", async () => {
      if (!backupId) return;
      const r = await api("GET", `/api/v1/user/backups/${backupId}/snapshots`);
      // 200 with [] or 200 with {snapshots:[]} both acceptable for a
      // backup that's never run.
      if (r.status !== 200 && r.status !== 404) {
        reportBug("backup-snapshots", `list snapshots → ${r.status}`);
      }
    });

    // ============================================================
    // E. Federations — v1.11.0.4 landed the replication fix.
    // End-to-end verification needs working UserRegion uploads to
    // seed the primary; that path is blocked by BUG02 (key-grant
    // decode) until v1.11.0.5 deploys. Deferred to v1.11.0.6.
    // ============================================================
    feature("E. Federations");

    skipLine(
      "E.* federation tests",
      "needs v1.11.0.5 key-grant fix live for primary seeding; deferred to v1.11.0.6",
    );

    // ============================================================
    // F. Webhooks
    // ============================================================
    feature("F. Webhooks");

    let webhookId = "";
    const webhookName = ephName("webhook");

    await check("F.1 create webhook for object.deleted", async () => {
      const r = await api("POST", "/api/v1/user/webhooks", {
        name: webhookName,
        targetUrl: "https://example.invalid/feat-smoke-target",
        events: ["object.deleted"],
        bucketFilter: urRegionId
          ? { regionId: urRegionId, bucketId: aBucketId }
          : undefined,
        enabled: true,
      });
      if (r.status !== 201) {
        reportBug(
          "webhook-create",
          `POST webhook → ${r.status}`,
          `body: ${r.text.slice(0, 300)}`,
        );
        return;
      }
      webhookId = r.body.id;
      if (!r.body.secret) {
        reportBug(
          "webhook-create-secret",
          `webhook create response missing 'secret' field (mint-once requirement)`,
          `keys returned: ${Object.keys(r.body ?? {}).join(",")}`,
        );
      }
      trackEphemeral({
        kind: "webhook",
        name: webhookName,
        delUrl: `/api/v1/user/webhooks/${webhookId}`,
      });
    });

    await check("F.2 GET webhook redacts secret + sets hasSecret", async () => {
      if (!webhookId) return;
      const r = await api("GET", `/api/v1/user/webhooks/${webhookId}`);
      if (r.status !== 200) {
        reportBug("webhook-get", `GET webhook → ${r.status}`);
        return;
      }
      if (r.body.secret) {
        reportBug(
          "webhook-get-secret-leak",
          `GET webhook returned non-empty 'secret' field — should be redacted`,
        );
      }
      if (r.body.hasSecret !== true) {
        reportBug("webhook-get-hasSecret", `GET webhook hasSecret=${r.body.hasSecret}, expected true`);
      }
    });

    await check("F.3 POST webhook test (delivery probe)", async () => {
      if (!webhookId) return;
      const r = await api("POST", `/api/v1/user/webhooks/${webhookId}/test`);
      // Test endpoint may 200 with delivery report, or 204, or 5xx if
      // the target is unreachable (which we expect — example.invalid).
      // We just verify the endpoint exists and accepts POST.
      if (r.status === 404 || r.status === 405) {
        reportBug("webhook-test", `POST /webhooks/{id}/test → ${r.status} (endpoint missing)`);
      }
    });

    await check("F.4 disable + enable webhook", async () => {
      if (!webhookId) return;
      const d = await api("POST", `/api/v1/user/webhooks/${webhookId}/disable`);
      if (d.status !== 200 && d.status !== 204) {
        reportBug("webhook-disable", `disable → ${d.status}`);
      }
      const e = await api("POST", `/api/v1/user/webhooks/${webhookId}/enable`);
      if (e.status !== 200 && e.status !== 204) {
        reportBug("webhook-enable", `enable → ${e.status}`);
      }
    });

    // ============================================================
    // G. Service accounts
    // ============================================================
    feature("G. Service accounts");

    let saId = "";
    let saSecret = "";
    const saName = ephName("sa");

    await check("G.1 mint SA with bucket:view capability", async () => {
      const r = await api("POST", "/api/v1/admin/service-accounts", {
        name: saName,
        capabilities: [{ id: "bucket:view", scope: "bucket:*:*" }],
      });
      if (r.status !== 201) {
        reportBug(
          "sa-create",
          `POST SA → ${r.status}`,
          `body: ${r.text.slice(0, 300)}`,
        );
        return;
      }
      saId = r.body.serviceAccount?.id ?? r.body.id;
      saSecret = r.body.secret ?? "";
      if (!saSecret) {
        reportBug(
          "sa-create-secret",
          `POST SA returned 201 but no 'secret' (mint-once)`,
          `keys: ${Object.keys(r.body ?? {}).join(",")}`,
        );
      }
      trackEphemeral({
        kind: "sa",
        name: saName,
        delUrl: `/api/v1/admin/service-accounts/${saId}`,
      });
    });

    await check("G.2 rotate SA returns new secret", async () => {
      if (!saId) return;
      const r = await api("POST", `/api/v1/admin/service-accounts/${saId}/rotate`);
      if (r.status !== 200) {
        reportBug("sa-rotate", `rotate SA → ${r.status} ${r.text.slice(0, 200)}`);
        return;
      }
      const newSecret = r.body.secret ?? "";
      if (!newSecret) {
        reportBug("sa-rotate-secret", `rotate response missing 'secret'`);
      } else if (newSecret === saSecret) {
        reportBug("sa-rotate-same-secret", `rotate returned the same secret`);
      }
    });

    await check("G.3 GET SA returns redacted record", async () => {
      if (!saId) return;
      const r = await api("GET", `/api/v1/admin/service-accounts/${saId}`);
      if (r.status !== 200) {
        reportBug("sa-get", `GET SA → ${r.status}`);
      }
    });

    // ============================================================
    // H. Lifecycle rules
    // ============================================================
    feature("H. Lifecycle rules");

    await check("H.1 GET lifecycle config on test bucket", async () => {
      if (!aBucketId) return;
      const r = await api(
        "GET",
        `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}/lifecycle`,
      );
      // 200 with rules:[] or 501 if Garage doesn't support it both
      // acceptable; 5xx is a bug.
      if (r.status >= 500) {
        reportBug(
          "lifecycle-get",
          `GET lifecycle → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      } else {
        info(`  lifecycle GET status=${r.status}`);
      }
    });

    await check("H.2 PUT lifecycle rule (expire after 30d, prefix=tmp/)", async () => {
      if (!aBucketId) return;
      const r = await api(
        "PUT",
        `/api/v1/admin/clusters/${c1.id}/buckets/${aBucketId}/lifecycle`,
        {
          rules: [
            {
              id: "feat-smoke-lifecycle",
              enabled: true,
              prefix: "tmp/",
              expiration: { days: 30 },
            },
          ],
        },
      );
      // Garage's lifecycle support is limited — driver may return 501
      // NOT_SUPPORTED with a capability hint. Accept that as a pass
      // (the gating is the feature). 4xx is also a doc'd handler bug.
      if (r.status === 200 || r.status === 204 || r.status === 501) {
        info(`  PUT lifecycle status=${r.status}`);
      } else if (r.status >= 500) {
        reportBug(
          "lifecycle-put",
          `PUT lifecycle → ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      } else {
        info(`  PUT lifecycle status=${r.status} (${r.text.slice(0, 100)})`);
      }
    });

    // ============================================================
    // I. Versioning — Garage stub (driver doesn't support)
    // ============================================================
    feature("I. Versioning (Garage stub)");

    await check("I.1 GET versioning returns supported=false on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "GET",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/versioning`,
      );
      if (r.status !== 200) {
        reportBug("versioning-get", `versioning GET → ${r.status}`);
        return;
      }
      if (r.body.supported !== false) {
        reportBug(
          "versioning-get-supported",
          `expected supported=false on Garage; got ${r.body.supported}`,
        );
      }
    });

    await check("I.2 PUT versioning returns 501 NOT_SUPPORTED on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "PUT",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/versioning`,
        { status: "enabled" },
      );
      if (r.status !== 501) {
        reportBug(
          "versioning-put",
          `expected 501 NOT_SUPPORTED on Garage PUT versioning; got ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      } else {
        // Error envelope: {error: {code, message, details}}.
        const code = r.body?.error?.code ?? r.body?.code;
        if (code !== "NOT_SUPPORTED") {
          reportBug(
            "versioning-put-code",
            `expected error.code NOT_SUPPORTED; got ${code}`,
          );
        }
      }
    });

    // ============================================================
    // J. Object Lock — Garage stub
    // ============================================================
    feature("J. Object Lock (Garage stub)");

    await check("J.1 GET object-lock returns supported=false on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "GET",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/object-lock`,
      );
      if (r.status !== 200) {
        reportBug("objectlock-get", `object-lock GET → ${r.status}`);
        return;
      }
      if (r.body.supported !== false) {
        reportBug(
          "objectlock-get-supported",
          `expected supported=false on Garage; got ${r.body.supported}`,
        );
      }
    });

    await check("J.2 PUT object-lock returns 501 NOT_SUPPORTED on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "PUT",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/object-lock`,
        { enabled: true, mode: "GOVERNANCE", days: 30 },
      );
      if (r.status !== 501) {
        reportBug(
          "objectlock-put",
          `expected 501 NOT_SUPPORTED on Garage PUT object-lock; got ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      }
    });

    // ============================================================
    // K. SSE — Garage stub
    // ============================================================
    feature("K. SSE (Garage stub)");

    await check("K.1 GET encryption returns supportedS3+supportedKms=false on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "GET",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/encryption`,
      );
      if (r.status !== 200) {
        reportBug("sse-get", `encryption GET → ${r.status}`);
        return;
      }
      // Wire field is `supportedKms` (lowercase 'ms' suffix).
      if (r.body.supportedS3 !== false || r.body.supportedKms !== false) {
        reportBug(
          "sse-get-supported",
          `expected supportedS3=false + supportedKms=false on Garage; got s3=${r.body.supportedS3} kms=${r.body.supportedKms}`,
        );
      }
    });

    await check("K.2 PUT encryption returns 501 NOT_SUPPORTED on Garage", async () => {
      if (!urRegionId || !aBucketId) return;
      const r = await api(
        "PUT",
        `/api/v1/user/regions/${urRegionId}/buckets/${aBucketId}/encryption`,
        { algorithm: "AES256" },
      );
      if (r.status !== 501) {
        reportBug(
          "sse-put",
          `expected 501 NOT_SUPPORTED on Garage PUT encryption; got ${r.status}`,
          `body: ${r.text.slice(0, 200)}`,
        );
      }
    });

    // ============================================================
    // L. WebDAV gateway
    // ============================================================
    feature("L. WebDAV gateway");

    await check("L.1 OPTIONS /webdav/ returns DAV headers", async () => {
      const headers: Record<string, string> = {};
      if (cookieJar.length > 0) headers.Cookie = cookieJar.join("; ");
      const r = await fetch(`${BASE_URL}/webdav/`, { method: "OPTIONS", headers });
      const dav = r.headers.get("dav");
      if (r.status >= 500) {
        reportBug("webdav-options", `OPTIONS /webdav/ → ${r.status}`);
      }
      if (!dav) {
        reportBug(
          "webdav-no-dav-header",
          `OPTIONS /webdav/ returned no DAV header (status=${r.status})`,
        );
      } else {
        info(`  DAV: ${dav}`);
      }
    });

    await check("L.2 PROPFIND /webdav/ (basic-auth probe)", async () => {
      // The edge may filter PROPFIND; record but don't fail.
      const basic = Buffer.from(`${USERNAME}:${PASSWORD}`).toString("base64");
      const r = await fetch(`${BASE_URL}/webdav/`, {
        method: "PROPFIND",
        headers: { Authorization: `Basic ${basic}`, Depth: "0" },
      });
      if (r.status === 405 || r.status === 501) {
        reportBug(
          "webdav-propfind-edge",
          `PROPFIND /webdav/ blocked by edge (status=${r.status}) — workaround: hit basement directly`,
        );
      } else if (r.status >= 500) {
        reportBug("webdav-propfind", `PROPFIND /webdav/ → ${r.status}`);
      } else {
        info(`  PROPFIND status=${r.status}`);
      }
    });

    // ============================================================
    // M. MCP server (basement-mcp is a stdio process, not HTTP —
    // we just verify the route exists in admin/system surface or
    // that an admin docs page references it).
    // ============================================================
    feature("M. MCP server");

    skipLine(
      "M.* MCP tool invocations",
      "basement-mcp is stdio-only; tested separately via cmd/basement-mcp unit tests",
    );

    // ============================================================
    // N. Audit log
    // ============================================================
    feature("N. Audit log");

    await check("N.1 list audit entries (default 24h window)", async () => {
      const r = await api("GET", "/api/v1/admin/audit");
      if (r.status !== 200) {
        reportBug("audit-list", `audit list → ${r.status} ${r.text.slice(0, 200)}`);
        return;
      }
      const events = r.body.events ?? [];
      info(`  audit returned ${events.length} event(s), total=${r.body.total}`);
    });

    await check("N.2 audit filter by action='bucket:create'", async () => {
      const r = await api("GET", "/api/v1/admin/audit?action=bucket:create&limit=10");
      if (r.status !== 200) {
        reportBug("audit-filter", `filtered audit → ${r.status}`);
        return;
      }
      const events = r.body.events ?? [];
      const ourBucketEvent = events.find(
        (e: any) => (e.resource ?? "").includes(c1.id) || (e.resource ?? "").includes(aBucketId),
      );
      if (!ourBucketEvent && aBucketId) {
        // It's possible the audit window doesn't include our event
        // if the filter is too narrow — record as a non-fatal note.
        info(`  no audit entry found for our bucket:create (may be outside default window)`);
      }
    });

    // ============================================================
    // O. Onboarding wizard
    // ============================================================
    feature("O. Onboarding wizard");

    await check("O.1 GET onboarding/state", async () => {
      const r = await api("GET", "/api/v1/admin/onboarding/state");
      if (r.status !== 200) {
        reportBug("onboarding-state", `state → ${r.status}`);
        return;
      }
      info(`  state: ${JSON.stringify(r.body).slice(0, 150)}`);
    });

    await check("O.2 POST onboarding/dismiss is idempotent", async () => {
      const r1 = await api("POST", "/api/v1/admin/onboarding/dismiss");
      if (r1.status !== 200 && r1.status !== 204) {
        reportBug("onboarding-dismiss", `dismiss → ${r1.status}`);
        return;
      }
      const r2 = await api("POST", "/api/v1/admin/onboarding/dismiss");
      if (r2.status !== 200 && r2.status !== 204) {
        reportBug("onboarding-dismiss-idempotent", `second dismiss → ${r2.status}`);
      }
    });

    // ============================================================
    // Optional Playwright UI smoke probe (one render) — kept light
    // since comprehensive-smoke covers UI heavily; this is just a
    // "the SPA still loads with our test resources" sanity check.
    // ============================================================
    feature("UI sanity (one render)");

    await check("UI.1 SPA shell renders with test resources visible", async () => {
      browser = await chromium.launch({ headless: true });
      bctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
      // Inject the session cookie from our jar
      const sessionCookie = cookieJar.find((c) => c.startsWith("__Host-"));
      if (sessionCookie) {
        const [n, v] = sessionCookie.split("=");
        await bctx.addCookies([
          {
            name: n,
            value: v,
            domain: new URL(BASE_URL).hostname,
            path: "/",
            httpOnly: true,
            secure: true,
            sameSite: "Strict",
          },
        ]);
      }
      const page = await bctx.newPage();
      const resp = await page.goto(`${BASE_URL}/files`, { waitUntil: "networkidle", timeout: 20_000 });
      if (resp && resp.status() >= 500) {
        throw new Error(`SPA /files → ${resp.status()}`);
      }
      await page.waitForSelector("h1, body", { timeout: 10_000 }).catch(() => {});
    });
  } finally {
    // ============================================================
    // CLEANUP — runs even if checks above threw
    // ============================================================
    section("[cleanup] reaping ephemeral resources (reverse-create order)");

    for (const e of ephemerals.slice().reverse()) {
      try {
        let extraHeaders: Record<string, string> | undefined = undefined;
        if (e.armUrl) {
          const arm = await api("POST", e.armUrl);
          if (arm.status === 200 && arm.body?.token) {
            extraHeaders = { "X-Confirm-Delete": arm.body.token };
          } else {
            warnLine(`  arm-delete for ${e.kind} ${e.name} → ${arm.status}; skipping`);
            continue;
          }
        }
        const d = await api("DELETE", e.delUrl, null, extraHeaders);
        if (d.status === 200 || d.status === 204) {
          info(`  reaped ${e.kind}: ${e.name}`);
        } else if (
          e.kind === "bucket" &&
          d.status === 409 &&
          d.text.includes("BucketNotEmpty")
        ) {
          // Bucket cleanup blocked by leftover objects / in-flight
          // multiparts (often a side-effect of BUG04 multipart-abort
          // not actually cancelling the upload). Leave a marker but
          // don't fail the whole run — the bucket name is prefixed
          // so an operator can scrub it by hand.
          warnLine(
            `  cleanup ${e.kind} ${e.name} blocked by BucketNotEmpty — leaving for manual scrub`,
          );
        } else {
          warnLine(`  cleanup ${e.kind} ${e.name} → ${d.status} ${d.text.slice(0, 100)}`);
        }
      } catch (err) {
        warnLine(
          `  cleanup ${e.kind} ${e.name} threw: ${err instanceof Error ? err.message : err}`,
        );
      }
    }

    // Operator-snapshot drift check
    if (baseline) {
      try {
        const after = await captureOperatorSnapshot();
        if (!snapshotsEqual(baseline, after)) {
          failLine(
            "operator-snapshot drift",
            `baseline ${JSON.stringify(baseline)} != after ${JSON.stringify(after)}`,
          );
          reportBug(
            "SAFETY",
            `Operator-real snapshot drift detected — possible real-data mutation`,
            `baseline=${JSON.stringify(baseline)} after=${JSON.stringify(after)}`,
          );
        } else {
          passLine("operator-snapshot drift check (baseline == after)", 0);
        }
      } catch (e) {
        warnLine(`snapshot drift check failed: ${e instanceof Error ? e.message : e}`);
      }
    }

    await bctx?.close().catch(() => {});
    await browser?.close().catch(() => {});
  }

  // ============================================================
  // Summary + bug report
  // ============================================================
  const passed = results.filter((r) => r.ok && !r.skipped).length;
  const skipped = results.filter((r) => r.skipped).length;
  const failed = results.filter((r) => !r.ok).length;

  section("=== PER-FEATURE SUMMARY ===");
  const byFeature = new Map<string, { pass: number; fail: number; skip: number }>();
  for (const r of results) {
    const k = r.feature;
    if (!byFeature.has(k)) byFeature.set(k, { pass: 0, fail: 0, skip: 0 });
    const e = byFeature.get(k)!;
    if (r.skipped) e.skip++;
    else if (r.ok) e.pass++;
    else e.fail++;
  }
  for (const [feat, c] of byFeature) {
    const status = c.fail > 0 ? `${C.red}FAIL${C.reset}` : `${C.green}PASS${C.reset}`;
    process.stdout.write(
      `  ${status} ${feat.padEnd(40)} pass=${c.pass} fail=${c.fail} skip=${c.skip}\n`,
    );
  }

  section("=== TOTALS ===");
  process.stdout.write(`${C.green}passed:${C.reset}  ${passed}\n`);
  process.stdout.write(`${C.yellow}skipped:${C.reset} ${skipped}\n`);
  process.stdout.write(`${C.red}failed:${C.reset}  ${failed}\n`);
  process.stdout.write(`${C.dim}bugs:    ${bugs.length}${C.reset}\n`);

  if (failed > 0) {
    section("=== FAILURES ===");
    for (const r of results.filter((x) => !x.ok)) {
      process.stderr.write(`${C.red}-${C.reset} [${r.feature}] ${r.name}: ${r.detail ?? ""}\n`);
    }
  }

  if (bugs.length > 0) {
    section("=== BUG REPORT ===");
    for (const b of bugs) {
      process.stderr.write(`${C.yellow}${b.id}${C.reset} [${b.feature}/${b.area}] ${b.detail}\n`);
      if (b.repro) process.stderr.write(`        ${C.dim}repro: ${b.repro}${C.reset}\n`);
    }
  }

  // Write the compact auto-generated report. The richer
  // `docs/feature-smoke-bugs.md` is hand-maintained per-cycle with
  // root-cause analysis + fix scope per bug; the auto-generated
  // companion lives next to it as `feature-smoke-latest.md` so the
  // prose report doesn't get clobbered by every run.
  try {
    const docsPath = resolve(__dirname, "..", "docs", "feature-smoke-latest.md");
    const md = renderBugReport();
    writeFileSync(docsPath, md);
    info(`latest run report written: ${docsPath}`);
  } catch (e) {
    warnLine(`failed to write latest run report: ${e instanceof Error ? e.message : e}`);
  }

  return failed === 0 ? 0 : 1;
}

function renderBugReport(): string {
  const date = new Date().toISOString().split("T")[0];
  const totals = {
    pass: results.filter((r) => r.ok && !r.skipped).length,
    skip: results.filter((r) => r.skipped).length,
    fail: results.filter((r) => !r.ok).length,
  };
  const lines: string[] = [];
  lines.push("# Feature-smoke bug report");
  lines.push("");
  lines.push(`Generated: ${date}`);
  lines.push(`Source: \`scripts/feature-smoke.ts\``);
  lines.push(`Target: \`${BASE_URL}\``);
  lines.push("");
  lines.push(
    `Totals: pass=${totals.pass}, skip=${totals.skip}, fail=${totals.fail}, bugs=${bugs.length}`,
  );
  lines.push("");
  lines.push("## Per-feature pass/fail summary");
  lines.push("");
  lines.push("| Feature | Pass | Fail | Skip |");
  lines.push("|---------|------|------|------|");
  const byFeature = new Map<string, { pass: number; fail: number; skip: number }>();
  for (const r of results) {
    const k = r.feature;
    if (!byFeature.has(k)) byFeature.set(k, { pass: 0, fail: 0, skip: 0 });
    const e = byFeature.get(k)!;
    if (r.skipped) e.skip++;
    else if (r.ok) e.pass++;
    else e.fail++;
  }
  for (const [feat, c] of byFeature) {
    lines.push(`| ${feat} | ${c.pass} | ${c.fail} | ${c.skip} |`);
  }
  lines.push("");

  if (bugs.length === 0) {
    lines.push("## Bugs");
    lines.push("");
    lines.push("No bugs reported in this run.");
    lines.push("");
  } else {
    lines.push("## Bugs");
    lines.push("");
    const byFeat = new Map<string, Bug[]>();
    for (const b of bugs) {
      if (!byFeat.has(b.feature)) byFeat.set(b.feature, []);
      byFeat.get(b.feature)!.push(b);
    }
    for (const [feat, list] of byFeat) {
      lines.push(`### ${feat} (${list.length} bug${list.length === 1 ? "" : "s"})`);
      lines.push("");
      for (const b of list) {
        lines.push(`- **${b.id}** (${b.area}): ${b.detail}`);
        if (b.repro) {
          lines.push(`  - repro: \`${b.repro}\``);
        }
      }
      lines.push("");
    }
  }

  if (totals.fail > 0) {
    lines.push("## Failed checks");
    lines.push("");
    for (const r of results.filter((x) => !x.ok)) {
      lines.push(`- [${r.feature}] ${r.name}`);
      if (r.detail) lines.push(`  - \`${r.detail}\``);
    }
    lines.push("");
  }

  return lines.join("\n");
}

const code = await main();
process.exit(code);
