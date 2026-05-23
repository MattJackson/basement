// v1.12.0a (ADR-0007) — global cluster-unlock prompt orchestration.
//
// Mirror of shared/auth/elevation.tsx but scoped to 423 LOCKED responses
// from the per-cluster envelope-encryption surface. Backend returns
// 423 LOCKED with details:{cluster_id, hint} whenever a mutation needs a
// CSK-decrypted secret on a locked cluster. The provider mounts the
// UnlockClusterModal once near the root; the client.ts middleware
// detects the 423 and calls promptUnlockFromAnywhere() to open it.
//
// Like ElevationProvider, this module deliberately keeps the wiring
// thin — there's a `runWithUnlock(cid, fn)` helper for explicit click
// handlers that prefer to retry-on-success, and the wire-level
// middleware is a safety net for code paths that haven't been wrapped
// yet.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { UnlockClusterModal } from "@/shared/ui/UnlockClusterModal";

// Module-level handle to the live prompt function. Same pattern as
// shared/auth/elevation.tsx — the provider registers on mount; non-
// React callers (client.ts middleware) read this without plumbing
// React context.
type UnlockPrompt = (cid: string, clusterLabel?: string) => Promise<void>;
let activePrompt: UnlockPrompt | null = null;

export function promptUnlockFromAnywhere(
  cid: string,
  clusterLabel?: string,
): Promise<void> {
  if (!activePrompt) {
    return Promise.reject(new Error("UNLOCK_PROVIDER_NOT_MOUNTED"));
  }
  return activePrompt(cid, clusterLabel);
}

/** isClusterLocked sniffs the error shape returned by apiError(). */
export function isClusterLocked(err: unknown): err is Error & {
  code: "LOCKED";
  details?: { cluster_id?: string; hint?: string };
} {
  if (!err || typeof err !== "object") return false;
  const e = err as { code?: string };
  return e.code === "LOCKED";
}

interface Pending {
  cid: string;
  clusterLabel?: string;
  resolve: () => void;
  reject: (err: unknown) => void;
}

interface UnlockContextValue {
  promptForUnlock: (cid: string, clusterLabel?: string) => Promise<void>;
  runWithUnlock: <T>(cid: string, op: () => Promise<T>) => Promise<T>;
}

const UnlockContext = createContext<UnlockContextValue | null>(null);

export function useClusterUnlockContext(): UnlockContextValue {
  const ctx = useContext(UnlockContext);
  if (!ctx) {
    throw new Error("useClusterUnlock* must be used inside <ClusterUnlockProvider>");
  }
  return ctx;
}

export function useClusterUnlockPrompt() {
  return useClusterUnlockContext().promptForUnlock;
}

export function useClusterUnlockGuard() {
  return useClusterUnlockContext().runWithUnlock;
}

export function ClusterUnlockProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const queueRef = useRef<Pending | null>(null);

  const promptForUnlock = useCallback(
    (cid: string, clusterLabel?: string) => {
      return new Promise<void>((resolve, reject) => {
        const next: Pending = { cid, clusterLabel, resolve, reject };
        // If something is already pending, immediately reject the new
        // request — concurrent unlocks for different clusters are not
        // a typical flow, and stacking modals would confuse the
        // operator. The caller of the second request can re-prompt
        // after the first settles.
        if (queueRef.current) {
          reject(new Error("UNLOCK_PROMPT_BUSY"));
          return;
        }
        queueRef.current = next;
        setPending(next);
      });
    },
    [],
  );

  const runWithUnlock = useCallback(
    async <T,>(cid: string, op: () => Promise<T>): Promise<T> => {
      try {
        return await op();
      } catch (err) {
        if (!isClusterLocked(err)) throw err;
        // Prompt + retry once on success.
        await promptForUnlock(cid);
        return op();
      }
    },
    [promptForUnlock],
  );

  // Register / deregister the module-level prompt hook.
  useEffect(() => {
    activePrompt = (cid: string, clusterLabel?: string) =>
      promptForUnlock(cid, clusterLabel);
    return () => {
      activePrompt = null;
    };
  }, [promptForUnlock]);

  const handleUnlocked = useCallback(() => {
    const cur = queueRef.current;
    queueRef.current = null;
    setPending(null);
    if (cur) cur.resolve();
  }, []);

  const handleCancel = useCallback(() => {
    const cur = queueRef.current;
    queueRef.current = null;
    setPending(null);
    if (cur) cur.reject(new Error("UNLOCK_CANCELLED"));
  }, []);

  const ctxValue = useMemo<UnlockContextValue>(
    () => ({ promptForUnlock, runWithUnlock }),
    [promptForUnlock, runWithUnlock],
  );

  return (
    <UnlockContext.Provider value={ctxValue}>
      {children}
      {pending ? (
        <UnlockClusterModal
          open={true}
          cid={pending.cid}
          clusterLabel={pending.clusterLabel ?? pending.cid}
          onUnlocked={handleUnlocked}
          onCancel={handleCancel}
        />
      ) : null}
    </UnlockContext.Provider>
  );
}
