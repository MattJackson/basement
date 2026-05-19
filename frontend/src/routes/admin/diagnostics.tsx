import { createFileRoute } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/diagnostics")({
  component: adminPage(DiagnosticsScreen),
});

function DiagnosticsScreen() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Diagnostics</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Logs, metrics, and probes for troubleshooting.
        </p>
      </header>
      <div className="flex items-center justify-center rounded-lg border bg-muted/50 py-24">
        <p className="text-sm text-muted-foreground">Diagnostics screen coming soon</p>
      </div>
    </div>
  );
}
