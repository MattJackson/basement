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

  useEffect(() => {
    return subscribe(setShow);
  }, []);

  if (!show) return null;

  return (
    <div className="sticky top-16 z-40 w-full bg-amber-500/10 border-b border-amber-500/30 text-amber-900 dark:text-amber-200">
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3 text-sm">
        <span>
          A new version of basement is available. Refresh to load it.
        </span>
        <Button
          size="sm"
          variant="outline"
          onClick={() => window.location.reload()}
        >
          Refresh
        </Button>
      </div>
    </div>
  );
}
