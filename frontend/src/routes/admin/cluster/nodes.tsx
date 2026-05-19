import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/admin/cluster/nodes")({
  component: NodesScreen,
});

function NodesScreen() {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">Nodes screen coming soon</p>
    </div>
  );
}
