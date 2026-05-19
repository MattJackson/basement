import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/keys")({
  component: KeysScreen,
});

function KeysScreen() {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">Keys screen coming soon</p>
    </div>
  );
}
