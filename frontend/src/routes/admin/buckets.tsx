import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/buckets")({
  component: BucketsScreen,
});

function BucketsScreen() {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">Buckets screen coming soon</p>
    </div>
  );
}
