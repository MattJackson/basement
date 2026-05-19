import { createFileRoute } from "@tanstack/react-router";
import AdminLanding from "./-AdminLanding";

export const Route = createFileRoute("/admin/")({
  component: AdminLanding,
});
