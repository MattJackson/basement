import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/diagnostics")({
  component: DiagnosticsScreen,
});

function DiagnosticsScreen() {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">Diagnostics screen coming soon</p>
    </div>
  );
}
