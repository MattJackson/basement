import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

export const Route = createFileRoute("/files/$cid")({
  component: userPage(ClusterBuckets),
});

function ClusterBuckets() {
  const { cid } = Route.useParams();
  
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Buckets</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Buckets in cluster {cid.slice(0, 8)}. Content lands in v0.6.0e.
        </p>
      </header>
    </div>
  );
}
