import { createFileRoute } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/cluster/layout")({
  component: adminPage(LayoutScreen),
});

function LayoutScreen() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Layout</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Stage and apply cluster layout changes.
        </p>
      </header>
      <div className="flex items-center justify-center rounded-lg border bg-muted/50 py-24">
        <p className="text-sm text-muted-foreground">Layout editor coming soon</p>
      </div>
    </div>
  );
}
