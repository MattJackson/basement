import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/login")({
  beforeLoad: async () => {
    throw redirect({ to: "/login" });
  },
});
