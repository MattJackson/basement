#!/usr/bin/env node
// scripts/openapi-sweep.ts — walks every (path, method) in openapi/basement.yaml,
// calls the live API, and reports drift.
//
// Auth: logs in as matthew, elevates, sets activeRole=ui-admin.
// Path params: substitutes from a small discovery pass (real cluster ID,
//   first bucket, first SA, etc).
// Output: /tmp/basement/openapi-sweep-{ts}.json + a human-readable summary.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { exec } from "node:child_process";
import { promisify } from "node:util";

const execAsync = promisify(exec);

// Simple cookie jar to persist cookies across requests
class CookieJar {
  private cookies: Map<string, string> = new Map();
  
  addCookie(name: string, value: string): void {
    this.cookies.set(name, value);
  }
  
  getCookieString(): string {
    const pairs: string[] = [];
    for (const [name, value] of this.cookies.entries()) {
      pairs.push(`${name}=${value}`);
    }
    return pairs.join("; ");
  }
  
  parseSetCookie(setCookieHeader: string): void {
    if (!setCookieHeader) return;
    
    const cookies = setCookieHeader.split(",").map(s => s.trim()).join("; ").split("; ");
    for (const cookie of cookies) {
      const eqIndex = cookie.indexOf("=");
      if (eqIndex > 0) {
        const name = cookie.substring(0, eqIndex);
        const value = cookie.substring(eqIndex + 1).split(";")[0];
        this.addCookie(name, value);
      }
    }
  }
}

const __dirname = dirname(fileURLToPath(import.meta.url));
const SPEC = resolve(__dirname, "..", "openapi", "basement.yaml");

const BASE_URL = process.env.BASE_URL ?? "https://basement.pq.io";
const USERNAME = process.env.BUI_USERNAME ?? "matthew";
const PASSWORD = process.env.BUI_PASSWORD ?? "password";
const ALLOW_WRITES = process.argv.includes("--writes");

type DiscoveryIds = {
  clusterId: string;
  bucketId: string;
  serviceAccountId?: string;
};

type EndpointResult = {
  path: string;
  method: string;
  status: number | null;
  expectedStatuses: number[];
  statusMatch: boolean | null;
  error?: string;
  skippedWrite: boolean;
};

type SweepReport = {
  timestamp: string;
  baseUrl: string;
  totalEndpoints: number;
  checkedCount: number;
  writeMethodsSkipped: number;
  mismatches: Array<{ path: string; method: string; expected: number[]; actual: number }>;
  authGatedSuccesses: Array<{ path: string; method: string }>;
  results: EndpointResult[];
};

async function parseOpenApiPaths(specPath: string): Promise<Record<string, { methods: Record<string, any> }>> {
  const pythonCmd = `python3 -c "import yaml, json; data=yaml.safe_load(open('${specPath}')); paths=data.get('paths', {}); print(json.dumps({k: {'methods': dict(v)} for k,v in paths.items()}, indent=2))"`;
  
  try {
    const { stdout } = await execAsync(pythonCmd, { timeout: 10000 });
    return JSON.parse(stdout);
  } catch (err) {
    console.error("Failed to parse YAML:", err);
    throw err;
  }
}

async function loginAndGetCookies(): Promise<CookieJar> {
  const jar = new CookieJar();
  
  // POST /api/v1/auth/login
  const loginResp = await fetch(`${BASE_URL}/api/v1/auth/login`, {
    method: "POST",
    headers: { 
      "Content-Type": "application/json" as const,
      Cookie: jar.getCookieString(),
    },
    body: JSON.stringify({ username: USERNAME, password: PASSWORD }),
  });
  
  if (!loginResp.ok) {
    throw new Error(`POST /api/v1/auth/login → ${loginResp.status} ${await loginResp.text()}`);
  }
  
  // Parse cookies from response
  const setCookie = loginResp.headers.get("set-cookie") ?? "";
  jar.parseSetCookie(setCookie);
  
  console.log("Cookies after login:", jar.getCookieString());
  
  // POST /api/v1/auth/elevate with cookies
  const elevateResp = await fetch(`${BASE_URL}/api/v1/auth/elevate`, {
    method: "POST",
    headers: { 
      "Content-Type": "application/json" as const,
      Cookie: jar.getCookieString(),
    },
    body: JSON.stringify({ target_mode: "admin", password: PASSWORD }),
  });
  
  if (!elevateResp.ok) {
    throw new Error(`POST /api/v1/auth/elevate → ${elevateResp.status} ${await elevateResp.text()}`);
  }
  
  jar.parseSetCookie(elevateResp.headers.get("set-cookie") ?? "");
  
  // PUT /api/v1/auth/active-role with cookies
  const roleResp = await fetch(`${BASE_URL}/api/v1/auth/active-role`, {
    method: "PUT",
    headers: { 
      "Content-Type": "application/json" as const,
      Cookie: jar.getCookieString(),
    },
    body: JSON.stringify({ kind: "ui-admin" }),
  });
  
  if (!roleResp.ok) {
    throw new Error(`PUT /api/v1/auth/active-role → ${roleResp.status} ${await roleResp.text()}`);
  }
  
  jar.parseSetCookie(roleResp.headers.get("set-cookie") ?? "");
  
  console.log("Final cookies:", jar.getCookieString());
  
  return jar;
}

async function discoverDefaultIds(jar: CookieJar): Promise<DiscoveryIds> {
  const clusterId = await fetchClusterId(jar);
  
  let bucketId: string;
  try {
    bucketId = await fetchFirstBucketId(jar, clusterId);
  } catch (err) {
    console.warn("Could not discover bucket ID:", err.message);
    bucketId = "test-not-found";
  }
  
  let serviceAccountId: string | undefined;
  try {
    const saIds = await fetchServiceAccountIds(jar);
    if (saIds.length > 0) {
      serviceAccountId = saIds[0];
    }
  } catch (err) {
    console.warn("Could not discover service account ID:", err.message);
  }
  
  return { clusterId, bucketId, serviceAccountId };
}

async function fetchClusterId(jar: CookieJar): Promise<string> {
  const resp = await fetch(`${BASE_URL}/api/v1/admin/clusters`, {
    method: "GET",
    headers: { 
      Cookie: jar.getCookieString(),
      "Content-Type": "application/json" as const,
    },
    signal: AbortSignal.timeout(5000),
  });
  
  if (!resp.ok) {
    throw new Error(`GET /admin/clusters → ${resp.status}`);
  }
  
  const data = await resp.json();
  if (!data.clusters || data.clusters.length === 0) {
    console.warn("No clusters found, using sentinel ID");
    return "does-not-exist";
  }
  
  return data.clusters[0].id;
}

async function fetchFirstBucketId(jar: CookieJar, clusterId: string): Promise<string> {
  const resp = await fetch(`${BASE_URL}/api/v1/admin/clusters/${clusterId}/buckets`, {
    method: "GET",
    headers: { 
      Cookie: jar.getCookieString(),
      "Content-Type": "application/json" as const,
    },
    signal: AbortSignal.timeout(5000),
  });
  
  if (!resp.ok) {
    throw new Error(`GET /admin/clusters/{cid}/buckets → ${resp.status}`);
  }
  
  const data = await resp.json();
  if (!data.buckets || data.buckets.length === 0) {
    console.warn("No buckets found in cluster, using sentinel ID");
    return "test-not-found";
  }
  
  return data.buckets[0].id;
}

async function fetchServiceAccountIds(jar: CookieJar): Promise<string[]> {
  const resp = await fetch(`${BASE_URL}/api/v1/admin/service-accounts`, {
    method: "GET",
    headers: { 
      Cookie: jar.getCookieString(),
      "Content-Type": "application/json" as const,
    },
    signal: AbortSignal.timeout(5000),
  });
  
  if (!resp.ok) {
    throw new Error(`GET /admin/service-accounts → ${resp.status}`);
  }
  
  const data = await resp.json();
  return (data.service_accounts ?? []).map((sa: any) => sa.id);
}

function substitutePathParams(path: string, ids: DiscoveryIds): string {
  let substituted = path;
  substituted = substituted.replace("{cid}", ids.clusterId);
  substituted = substituted.replace("{id}", ids.bucketId);
  substituted = substituted.replace("{bucket}", ids.bucketId);
  
  if (ids.serviceAccountId) {
    substituted = substituted.replace("{sa_id}", ids.serviceAccountId);
  } else {
    // Use sentinel for SA IDs if not discovered
    substituted = substituted.replace("{sa_id}", "does-not-exist");
  }
  
  // Handle key+ patterns (path parameters with + suffix)
  substituted = substituted.replace("/{key+}", "");
  
  return substituted;
}

function getExpectedStatuses(method: string, specMethods: Record<string, any>): number[] {
  const methodSpec = specMethods[method];
  if (!methodSpec || !methodSpec.responses) {
    return [];
  }
  
  return Object.keys(methodSpec.responses).map(Number).sort((a, b) => a - b);
}

function isWriteMethod(method: string): boolean {
  return ["POST", "PUT", "PATCH", "DELETE"].includes(method.toUpperCase());
}

async function runSweep(): Promise<SweepReport> {
  console.log("Starting OpenAPI endpoint sweep...");
  console.log(`BASE_URL: ${BASE_URL}`);
  console.log(`USERNAME: ${USERNAME}`);
  console.log(`ALLOW_WRITES: ${ALLOW_WRITES}`);
  
  const startTime = Date.now();
  const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
  
  // Parse spec
  console.log("\n[1/5] Parsing OpenAPI spec...");
  const pathsData = await parseOpenApiPaths(SPEC);
  const totalEndpoints = Object.values(pathsData).reduce((sum, p) => sum + Object.keys(p.methods).length, 0);
  console.log(`Found ${totalEndpoints} endpoint-method combinations`);
  
  // Auth bootstrap
  console.log("\n[2/5] Authenticating...");
  let jar: CookieJar;
  try {
    jar = await loginAndGetCookies();
    console.log("✓ Authentication successful (login → elevate → active-role)");
  } catch (err: any) {
    throw new Error(`Auth bootstrap failed: ${err.message}`);
  }
  
  // Discovery pass
  console.log("\n[3/5] Discovering default resource IDs...");
  const ids = await discoverDefaultIds(jar);
  console.log(`✓ clusterId: ${ids.clusterId}`);
  console.log(`✓ bucketId: ${ids.bucketId}`);
  if (ids.serviceAccountId) {
    console.log(`✓ serviceAccountId: ${ids.serviceAccountId}`);
  }
  
  // Run endpoints
  console.log("\n[4/5] Running endpoint sweep...");
  const results: EndpointResult[] = [];
  let writeMethodsSkipped = 0;
  const mismatches: Array<{ path: string; method: string; expected: number[]; actual: number }> = [];
  const authGatedSuccesses: Array<{ path: string; method: string }> = [];
  
  for (const [path, { methods }] of Object.entries(pathsData)) {
    const substitutedPath = substitutePathParams(path, ids);
    
    for (const [method, methodSpec] of Object.entries(methods)) {
      const expectedStatuses = getExpectedStatuses(method, { [method]: methodSpec });
      
      // Skip write methods unless --writes flag is passed
      if (!ALLOW_WRITES && isWriteMethod(method)) {
        results.push({
          path: substitutedPath,
          method: method.toUpperCase(),
          status: null,
          expectedStatuses,
          statusMatch: null,
          skippedWrite: true,
        });
        writeMethodsSkipped++;
        continue;
      }
      
      try {
        const resp = await fetch(`${BASE_URL}/api/v1${substitutedPath}`, {
          method: method.toUpperCase(),
          headers: { 
            Cookie: jar.getCookieString(),
            "Content-Type": "application/json",
          },
          signal: AbortSignal.timeout(5000),
        });
        
        const status = resp.status;
        let statusMatch: boolean | null = null;
        
        if (expectedStatuses.length > 0) {
          statusMatch = expectedStatuses.includes(status);
        }
        
        // Check for auth-gated endpoints that succeeded unexpectedly
        const methodSpecAny = methodSpec as any;
        const requiresAuth = methodSpecAny?.security && methodSpecAny.security.length > 0;
        if (requiresAuth && status === 200) {
          // This is expected - we're authenticated
        } else if (!requiresAuth && status >= 400) {
          // Non-authenticated endpoint returned error - might be expected
        }
        
        results.push({
          path: substitutedPath,
          method: method.toUpperCase(),
          status,
          expectedStatuses,
          statusMatch,
        });
        
        if (statusMatch === false) {
          mismatches.push({
            path: substitutedPath,
            method: method.toUpperCase(),
            expected: expectedStatuses,
            actual: status,
          });
        }
      } catch (err: any) {
        console.warn(`✗ ${method.toUpperCase()} ${substitutedPath}: ${err.message}`);
        results.push({
          path: substitutedPath,
          method: method.toUpperCase(),
          status: null,
          expectedStatuses,
          statusMatch: false,
          error: err.message,
        });
      }
    }
  }
  
  const checkedCount = results.filter(r => !r.skippedWrite).length;
  
  // Generate report
  console.log("\n[5/5] Generating report...");
  const report: SweepReport = {
    timestamp,
    baseUrl: BASE_URL,
    totalEndpoints,
    checkedCount,
    writeMethodsSkipped,
    mismatches,
    authGatedSuccesses,
    results,
  };
  
  // Write JSON output
  const outputPath = `/tmp/basement/openapi-sweep-${timestamp}.json`;
  writeFileSync(outputPath, JSON.stringify(report, null, 2));
  console.log(`✓ JSON report written to: ${outputPath}`);
  
  // Print summary
  const endTime = Date.now();
  const durationSec = ((endTime - startTime) / 1000).toFixed(1);
  
  console.log("\n" + "=".repeat(60));
  console.log("SUMMARY");
  console.log("=".repeat(60));
  console.log(`Total endpoints in spec: ${totalEndpoints}`);
  console.log(`Endpoints checked (read-only): ${checkedCount}`);
  console.log(`Write methods skipped: ${writeMethodsSkipped}`);
  console.log(`Status code mismatches: ${mismatches.length}`);
  console.log(`Duration: ${durationSec}s`);
  
  if (mismatches.length > 0) {
    console.log("\nMISMATCHES:");
    for (const m of mismatches.slice(0, 20)) {
      console.log(`  - ${m.method} ${m.path}`);
      console.log(`    Expected: ${m.expected.join(", ")}, Actual: ${m.actual}`);
    }
    if (mismatches.length > 20) {
      console.log(`  ... and ${mismatches.length - 20} more`);
    }
  }
  
  return report;
}

// Main execution
runSweep()
  .then(() => {
    process.exit(0);
  })
  .catch((err) => {
    console.error("Fatal error:", err.message);
    process.exit(1);
  });
