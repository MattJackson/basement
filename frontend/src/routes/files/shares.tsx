import { createFileRoute } from "@tanstack/react-router";

function SharesPlaceholder() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Shares</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Shares — coming in v0.7
        </p>
      </header>
    </div>
  );
}

export const Route = createFileRoute("/files/shares")({
  component: SharesPlaceholder,
});

export default SharesPlaceholder;
