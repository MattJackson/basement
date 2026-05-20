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

declare const __BUILD_COMMIT__: string;

const SKIP_VALUES = new Set(["", "dev", "unknown"]);

let mismatched = false;
const listeners = new Set<(m: boolean) => void>();

export function observeResponse(res: Response): void {
  if (mismatched) return;
  const serverBuild = res.headers.get("x-build");
  if (!serverBuild || SKIP_VALUES.has(serverBuild)) return;
  if (SKIP_VALUES.has(__BUILD_COMMIT__)) return;
  if (serverBuild === __BUILD_COMMIT__) return;
  mismatched = true;
  for (const l of listeners) l(true);
}

export function subscribe(fn: (m: boolean) => void): () => void {
  listeners.add(fn);
  fn(mismatched);
  return () => listeners.delete(fn);
}

export function getBuildCommit(): string {
  return __BUILD_COMMIT__;
}
