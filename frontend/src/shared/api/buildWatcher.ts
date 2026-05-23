// buildWatcher detects when the server has deployed a newer build than
// the currently-loaded bundle. Hooked into every API response via the
// fetch interceptor in client.ts.
//
// Flow:
//   1. Vite stamps __BUILD_COMMIT__ at build time (see vite.config.ts).
//   2. Every API response carries X-Build: <server commit>.
//   3. observeResponse() compares the two; on mismatch it flips
//      mismatched=true and notifies subscribers (the banner).
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
const listeners = new Set<(m: boolean) => void>();
let heartbeatInterval: number | null = null;

function checkBuild(res: Response): void {
  if (mismatched) return;
  const serverBuild = res.headers.get("x-build");
  if (!serverBuild || SKIP_VALUES.has(serverBuild)) return;
  if (SKIP_VALUES.has(__BUILD_COMMIT__)) return;
  if (serverBuild === __BUILD_COMMIT__) return;
  mismatched = true;
  for (const l of listeners) l(true);
}

export function observeResponse(res: Response): void {
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

export function subscribe(fn: (m: boolean) => void): () => void {
  listeners.add(fn);
  fn(mismatched);
  
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
  return __BUILD_COMMIT__;
}

// Exposed for tests
export function __stopHeartbeatForTests(): void {
  stopHeartbeat();
}
