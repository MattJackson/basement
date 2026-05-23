import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/login")({
  beforeLoad: async () => {
    // Use window.location.replace for true 301 redirect behavior
    // TanStack Router's redirect() doesn't support custom status codes
    if (typeof window !== "undefined") {
      window.location.replace("/login");
    }
    throw new Error("Redirect to /login");
  },
});
