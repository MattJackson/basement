import { createFileRoute } from "@tanstack/react-router";
import { LoginForm } from "@/shared/auth/LoginForm";

type LoginSearch = { next?: string };

export const Route = createFileRoute("/admin/login")({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    next: typeof search.next === "string" ? search.next : undefined,
  }),
  component: LoginForm,
});
