/**
 * sanitizeNextPath guards the post-login `?next=` redirect target against
 * open-redirect attacks.
 *
 * An attacker can craft `?next=//evil.com` or `?next=/\evil.com`. Both
 * start with "/", so a naive `startsWith("/")` check accepts them — but
 * the browser (and TanStack Router's `navigate({to})`) resolves a
 * protocol-relative URL like `//evil.com` to `https://evil.com`,
 * navigating off-origin. `/\evil.com` is normalized to `//evil.com` by
 * some user agents, so it's equally dangerous.
 *
 * A safe in-app path must:
 *   - start with a single "/"
 *   - NOT start with "//" (protocol-relative)
 *   - NOT start with "/\" (backslash variant some browsers normalize)
 *
 * Returns the path if safe, otherwise `null`. Callers pick their own
 * fallback (LoginForm defaults to "/files").
 */
export function sanitizeNextPath(next: unknown): string | null {
  if (typeof next !== "string") return null;
  if (!next.startsWith("/")) return null;
  // Reject protocol-relative ("//host") and the backslash variant ("/\host").
  if (next.startsWith("//") || next.startsWith("/\\")) return null;
  return next;
}
