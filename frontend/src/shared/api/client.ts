import createClient from "openapi-fetch";
import type { paths } from "./types.gen";

export const client = createClient<paths>({
  baseUrl: import.meta.env.VITE_API_BASE ?? "/api/v1",
  credentials: "include",
  headers: {
    Accept: "application/json",
  },
});
