// ADR-0003 / v1.2.0b — global elevation prompt orchestration.
//
// The backend returns 403 + code=ELEVATION_REQUIRED whenever the
// caller holds a capability but their current mode is below the
// minimum required (USER trying cluster:delete, ADMIN trying it after
// 5min idle, etc.). This module wires that into a single shared
// modal:
//
//   - <ElevationProvider> mounts the modal once near the root and
//     holds the "what to retry on success" queue.
//   - useElevationGuard() returns a `runWithElevation(fn)` helper
//     that wraps any async operation. If the wrapped op throws an
//     ELEVATION_REQUIRED error, the modal opens; on a successful
//     re-auth the op is invoked once more and its result resolves the
//     original promise. On user cancel, the original error rethrows.
//
// We deliberately don't reach into tanstack-query internals — every
// destructive op the v1.2.0b scope cares about is an explicit click
// handler, so wrapping the click handler in runWithElevation keeps
// the call-site code obvious and the global plumbing thin.

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
import { useQueryClient } from "@tanstack/react-query";
import { ElevationModal } from "@/components/auth/ElevationModal";
import type { AuthMode } from "@/shared/auth/mode";

// Module-level handle to the live promptForElevation function. The
// provider registers itself on mount; non-React callers (openapi-fetch
// middleware, fetch wrappers, etc.) read this to surface the modal
// without having to plumb React context. Null when no provider is
// mounted yet (during initial paint of a logged-out shell).
type AnyPrompt = (
  target: Exclude<AuthMode, "user">,
  reason?: string,
) => Promise<void>;
let activePrompt: AnyPrompt | null = null;

/** promptElevationFromAnywhere is the non-React entrypoint. */
export function promptElevationFromAnywhere(
  target: Exclude<AuthMode, "user">,
  reason?: string,
): Promise<void> {
  if (!activePrompt) {
    return Promise.reject(new Error("ELEVATION_PROVIDER_NOT_MOUNTED"));
  }
  return activePrompt(target, reason);
}

/** isElevationRequired sniffs the error shape returned by api/queries.ts apiError(). */
export function isElevationRequired(err: unknown): err is Error & {
  code: "ELEVATION_REQUIRED";
  details?: { mode_required?: AuthMode; current_mode?: AuthMode };
} {
  if (!err || typeof err !== "object") return false;
  const e = err as { code?: string };
  return e.code === "ELEVATION_REQUIRED";
}

interface PendingPrompt {
  /** The target_mode to send to /auth/elevate (ADMIN or ELEVATED). */
  targetMode: Exclude<AuthMode, "user">;
  /** Optional context to render in the modal body. */
  reason?: string;
  /** Resolve fires after successful elevation; reject fires on user cancel. */
  resolve: () => void;
  reject: (err: unknown) => void;
}

interface ElevationContextValue {
  /** Promise that resolves only after the user successfully elevates. */
  promptForElevation: (
    targetMode: Exclude<AuthMode, "user">,
    reason?: string,
  ) => Promise<void>;
  /**
   * Wrap any async op so a thrown ELEVATION_REQUIRED triggers the
   * modal and the op is retried once on success. The original error
   * rethrows on cancel.
   */
  runWithElevation: <T>(op: () => Promise<T>) => Promise<T>;
}

const ElevationContext = createContext<ElevationContextValue | null>(null);

export function useElevationContext(): ElevationContextValue {
  const ctx = useContext(ElevationContext);
  if (!ctx) {
    throw new Error("useElevation* must be used inside <ElevationProvider>");
  }
  return ctx;
}

/** useElevationPrompt opens the modal explicitly (e.g. UserMenu "Switch to admin"). */
export function useElevationPrompt() {
  return useElevationContext().promptForElevation;
}

/** useElevationGuard returns the runWithElevation wrapper. */
export function useElevationGuard() {
  return useElevationContext().runWithElevation;
}

export function ElevationProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<PendingPrompt | null>(null);
  // queueRef stores a single in-flight prompt; if a second op fires
  // mid-modal we surface its 403 directly (no nested modals). In
  // practice the modal blocks the UI so concurrent prompts shouldn't
  // arise — but the ref prevents a state race if they do.
  const queueRef = useRef<PendingPrompt | null>(null);
  const queryClient = useQueryClient();

  const promptForElevation = useCallback(
    (targetMode: Exclude<AuthMode, "user">, reason?: string) =>
      new Promise<void>((resolve, reject) => {
        const entry: PendingPrompt = { targetMode, reason, resolve, reject };
        queueRef.current = entry;
        setPending(entry);
      }),
    [],
  );

  const runWithElevation = useCallback(
    async <T,>(op: () => Promise<T>): Promise<T> => {
      try {
        return await op();
      } catch (err) {
        if (!isElevationRequired(err)) throw err;
        const required = err.details?.mode_required;
        const target: Exclude<AuthMode, "user"> =
          required === "elevated" ? "elevated" : "admin";
        await promptForElevation(target, "This action requires re-authentication.");
        // Retry once after successful elevation. If the retry itself
        // throws ELEVATION_REQUIRED again (server-side TTL race) we
        // surface that to the caller — no infinite loop here.
        return op();
      }
    },
    [promptForElevation],
  );

  const handleSuccess = useCallback(() => {
    const entry = queueRef.current;
    queueRef.current = null;
    setPending(null);
    entry?.resolve();
    // v1.9.0e.1: invalidate /auth/me (and any mode-derived queries) so
    // the AuthModeHydrator picks up the fresh server mode immediately.
    // Without this, the previously-cached /auth/me payload still says
    // mode=user and AdminEntryElevationGuard fires the modal AGAIN on
    // the very next render — same shape as the v1.7.0a.2 drop-
    // privileges cache-staleness bug, just on the rising edge.
    queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
    queryClient.invalidateQueries({ queryKey: ["user"] });
  }, [queryClient]);

  const handleCancel = useCallback(() => {
    const entry = queueRef.current;
    queueRef.current = null;
    setPending(null);
    entry?.reject(new Error("ELEVATION_CANCELLED"));
  }, []);

  // Register/unregister the non-React prompt entrypoint so middleware
  // can open the modal without touching React context. Stays a simple
  // ref-counted singleton — the only producer is this provider, the
  // only consumer is the openapi-fetch middleware in client.ts.
  useEffect(() => {
    activePrompt = promptForElevation;
    return () => {
      if (activePrompt === promptForElevation) {
        activePrompt = null;
      }
    };
  }, [promptForElevation]);

  const value = useMemo<ElevationContextValue>(
    () => ({ promptForElevation, runWithElevation }),
    [promptForElevation, runWithElevation],
  );

  return (
    <ElevationContext.Provider value={value}>
      {children}
      <ElevationModal
        open={pending !== null}
        targetMode={pending?.targetMode ?? "admin"}
        reason={pending?.reason}
        onSuccess={handleSuccess}
        onCancel={handleCancel}
      />
    </ElevationContext.Provider>
  );
}
