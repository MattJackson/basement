import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

export const Route = createFileRoute("/files/")({
  component: userPage(FilesHome),
});

function FilesHome() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Clusters</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Clusters you have access to. Content lands in v0.6.0d.
        </p>
      </header>
    </div>
  );
}
