import createClient from "openapi-fetch";
import type { paths } from "./types.gen";
import { observeResponse } from "./buildWatcher";
import { promptElevationFromAnywhere } from "@/shared/auth/elevation";
import type { AuthMode } from "@/shared/auth/mode";

export const client = createClient<paths>({
  baseUrl: import.meta.env.VITE_API_BASE ?? "/api/v1",
  credentials: "include",
  headers: {
    Accept: "application/json",
  },
});

// Feed every API response through the build watcher so the
// version-mismatch banner can fire on the first organic API call after
// a deploy. openapi-fetch v0.10+ exposes onResponse via use().
client.use({
  onResponse({ response }) {
    observeResponse(response);
  },
});

// ADR-0003 v1.2.0b: detect 403 ELEVATION_REQUIRED at the wire level
// and open the global modal eagerly. This is the safety net for code
// paths that haven't wrapped themselves in useElevationGuard — the
// caller still sees the original 403 error and decides what to do,
// but at minimum the user is shown the password prompt instead of a
// silent failure. Wrapped click handlers get a smoother retry via
// useElevationGuard.
//
// We peek at the body without consuming it (clone first) because
// openapi-fetch hasn't parsed it yet at the onResponse boundary.
client.use({
  async onResponse({ response }) {
    if (response.status !== 403) return;
    try {
      const cloned = response.clone();
      const body = (await cloned.json().catch(() => null)) as
        | {
            error?: {
              code?: string;
              details?: { mode_required?: AuthMode };
            };
          }
        | null;
      if (body?.error?.code !== "ELEVATION_REQUIRED") return;
      const required = body.error.details?.mode_required;
      const target: Exclude<AuthMode, "user"> =
        required === "elevated" ? "elevated" : "admin";
      // Fire-and-forget: a failure here (no provider mounted) is
      // already conveyed as the original 403 to the caller.
      promptElevationFromAnywhere(target).catch(() => {});
    } catch {
      // Ignore body-parse failures — they imply a non-JSON 403
      // (unlikely for our backend) and there's nothing useful to do.
    }
  },
});
