import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/cluster/layout")({
  component: LayoutScreen,
});

function LayoutScreen() {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">Layout editor coming soon</p>
    </div>
  );
}
