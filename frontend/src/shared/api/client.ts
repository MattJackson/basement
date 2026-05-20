import createClient from "openapi-fetch";
import type { paths } from "./types.gen";
import { observeResponse } from "./buildWatcher";

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
