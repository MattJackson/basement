/**
 * apiError builds a user-presentable Error from a non-2xx response.
 * Uses the uniform error shape from design.md (`{error:{code,message,details}}`)
 * so screens can show the upstream cause instead of a generic message.
 *
 * `code` and `status` are attached to the Error. `details` is forwarded
 * onto the Error object when present so screens that need a structured
 * payload (e.g. the elevation guard reading `details.mode_required`, or
 * NO_ADMIN_BRIDGE surfacing the offending endpoint + field) can read it
 * without re-fetching.
 *
 * This is the single canonical implementation — both queries.ts and
 * mutations.ts import it. Do NOT re-define a local copy (a previous
 * divergent mutations.ts copy dropped `details`, which broke the
 * elevation guard's admin-vs-elevated targeting on delete ops).
 */
export type ApiErrorWithMeta = Error & {
  code?: string;
  status?: number;
  details?: Record<string, unknown>;
};

export function apiError(
  resource: string,
  status: number,
  body: unknown,
): Error {
  let code = `HTTP_${status}`;
  let message = `${resource} failed (HTTP ${status})`;
  let details: Record<string, unknown> | undefined;
  if (body && typeof body === "object" && "error" in body) {
    const e = (body as { error?: { code?: string; message?: string; details?: Record<string, unknown> } }).error;
    if (e?.code) code = e.code;
    if (e?.message) message = e.message;
    if (e?.details) details = e.details;
  }
  const err = new Error(`${code}: ${message}`) as ApiErrorWithMeta;
  err.code = code;
  err.status = status;
  if (details) {
    err.details = details;
  }
  return err;
}
