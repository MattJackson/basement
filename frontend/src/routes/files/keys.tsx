import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

export const Route = createFileRoute("/files/keys")({
  component: userPage(KeysHome),
});

function KeysHome() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Keys grouped by (cluster, bucket) pair. Content lands in v0.6.0f.
        </p>
      </header>
    </div>
  );
}
