// buildWatcher detects when the server has deployed a newer build than
// the currently-loaded bundle. Hooked into every API response via the
// fetch interceptor in client.ts.
//
// Flow:
//   1. Vite stamps __BUILD_COMMIT__ at build time (see vite.config.ts).
//   2. Every API response carries X-Build: <server commit> and X-Version: <tag>.
//   3. observeResponse() compares the two; on mismatch it flips
//      mismatched=true and notifies subscribers (the banner) with version info.
//
// "dev" / "unknown" / empty values are treated as "don't compare" —
// local dev builds have a "dev" baked commit and would otherwise flag
// every response from a real server.
//
// Heartbeat: When at least one subscriber listens, starts polling
// /api/v1/version every 60s to detect new deploys even when the user
// is idle on a page with no fetch activity. Stops once mismatch detected.

declare const __BUILD_COMMIT__: string;

const HEARTBEAT_MS = 60_000;
const SKIP_VALUES = new Set(["", "dev", "unknown"]);

let mismatched = false;
let serverVersion: string | undefined = undefined;
const listeners = new Set<(state: { mismatched: boolean; serverVersion?: string }) => void>();
let heartbeatInterval: number | null = null;

function checkBuild(res: Response): void {
  if (mismatched) return;
  const serverBuild = res.headers.get("x-build");
  if (!serverBuild || SKIP_VALUES.has(serverBuild)) return;
  
  // Use globalThis for test compatibility
  const clientBuild = typeof __BUILD_COMMIT__ !== "undefined" ? __BUILD_COMMIT__ : (globalThis as any).__BUILD_COMMIT__;
  if (SKIP_VALUES.has(clientBuild)) return;
  if (serverBuild === clientBuild) return;
  
  mismatched = true;
  for (const l of listeners) l({ mismatched: true, serverVersion });
}

export function observeResponse(res: Response): void {
  const versionHeader = res.headers.get("x-version");
  if (versionHeader && !SKIP_VALUES.has(versionHeader)) {
    serverVersion = versionHeader;
    for (const l of listeners) l({ mismatched, serverVersion });
  }
  checkBuild(res);
}

function startHeartbeat(): void {
  if (heartbeatInterval !== null || mismatched) return;
  
  const fetchVersion = async () => {
    try {
      const res = await fetch("/api/v1/version", { credentials: "include" });
      checkBuild(res);
    } catch {
      // Silently swallow failures; retry on next tick
    }
  };

  heartbeatInterval = setInterval(fetchVersion, HEARTBEAT_MS);
}

function stopHeartbeat(): void {
  if (heartbeatInterval !== null) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
  }
}

export function subscribe(fn: (state: { mismatched: boolean; serverVersion?: string }) => void): () => void {
  listeners.add(fn);
  fn({ mismatched, serverVersion });
  
  if (!mismatched && listeners.size === 1) {
    startHeartbeat();
  }
  
  return () => {
    listeners.delete(fn);
    if (listeners.size === 0) {
      stopHeartbeat();
    }
  };
}

export function getBuildCommit(): string {
  // Use globalThis for test compatibility
  return typeof __BUILD_COMMIT__ !== "undefined" ? __BUILD_COMMIT__ : ((globalThis as any).__BUILD_COMMIT__ || "");
}

// Exposed for tests
export function __stopHeartbeatForTests(): void {
  stopHeartbeat();
}
