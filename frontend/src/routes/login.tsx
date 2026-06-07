import { createFileRoute } from "@tanstack/react-router";
import { LoginForm } from "@/shared/auth/LoginForm";
import { sanitizeNextPath } from "@/shared/lib/safeNext";

type LoginSearch = { next?: string };

export const Route = createFileRoute("/login")({
  // Sanitize `next` at the search-validation boundary so an unsafe value
  // (`//evil.com`, `/\evil.com`, or any off-origin string) never reaches
  // the component as a usable redirect target. Drop it to undefined.
  validateSearch: (search: Record<string, unknown>): LoginSearch => {
    const next = sanitizeNextPath(search.next);
    return next ? { next } : {};
  },
  component: LoginForm,
});
