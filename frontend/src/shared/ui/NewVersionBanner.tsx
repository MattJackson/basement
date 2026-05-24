import { useEffect, useState } from "react";
import { subscribe } from "@/shared/api/buildWatcher";
import { Button } from "@/components/ui/button";

/**
 * NewVersionBanner subscribes to the build watcher and renders a
 * sticky bar when the server's X-Build header diverges from the
 * baked __BUILD_COMMIT__. A click hard-reloads the page so the
 * browser fetches the new index.html (Cache-Control: no-store on
 * the entry HTML guarantees a fresh response) and pulls the new
 * hashed bundles.
 */
export function NewVersionBanner() {
  const [show, setShow] = useState(false);
  const [version, setVersion] = useState<string | undefined>(undefined);

  useEffect(() => {
    return subscribe((state) => {
      setShow(state.mismatched);
      setVersion(state.serverVersion);
    });
  }, []);

  if (!show) return null;

  const message = version 
    ? `Basement ${version} now available — Refresh`
    : "A new version of basement is available. Refresh to load it.";

  async function handleRefresh() {
    try {
      if ("serviceWorker" in navigator) {
        const regs = await navigator.serviceWorker.getRegistrations();
        await Promise.all(regs.map((r) => r.unregister()));
      }
      if ("caches" in window) {
        const keys = await caches.keys();
        await Promise.all(keys.map((k) => caches.delete(k)));
      }
    } catch {
      // best-effort; reload anyway
    }
    window.location.reload();
  }

  return (
    <div className="sticky top-16 z-40 w-full bg-amber-500/10 border-b border-amber-500/30 text-amber-900 dark:text-amber-200">
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3 text-sm">
        <span>{message}</span>
        <Button size="sm" variant="outline" onClick={handleRefresh}>
          Refresh
        </Button>
      </div>
    </div>
  );
}
