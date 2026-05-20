import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { useUserSyncs, useDeleteUserSync, usePauseUserSync, useResumeUserSync } from "@/shared/api/queries";
import { StartSyncDialog } from "@/components/sync/StartSyncDialog";

function Progress({ value }: { value: number }) {
  return (
    <div className="w-full bg-muted rounded-full h-2">
      <div 
        className="bg-primary h-2 rounded-full transition-all" 
        style={{ width: `${value}%` }}
      />
    </div>
  );
}

export const Route = createFileRoute("/files/syncs")({
  component: SyncsPage,
});

function SyncsPage() {
  const [dialogOpen, setDialogOpen] = useState(false);
  const deleteMutation = useDeleteUserSync();
  const pauseMutation = usePauseUserSync();
  const resumeMutation = useResumeUserSync();

  const { data: jobs = [], isLoading } = useUserSyncs();

  const handleDelete = (id: string) => {
    if (confirm("Are you sure you want to delete this sync job?")) {
      deleteMutation.mutateAsync(id);
    }
  };

  const handlePause = async (id: string, currentState: string) => {
    if (currentState === "running" || currentState === "queued") {
      await pauseMutation.mutateAsync(id);
    }
  };

  const handleResume = async (id: string) => {
    await resumeMutation.mutateAsync(id);
  };

  const getStateColor = (state: string) => {
    switch (state) {
      case "done":
        return "text-green-600";
      case "error":
        return "text-red-600";
      case "paused":
        return "text-yellow-600";
      default:
        return "text-blue-600";
    }
  };

  const getProgressPercent = (job: any) => {
    if (job.progress.objects_total === 0) return 0;
    return Math.round((job.progress.objects_copied / job.progress.objects_total) * 100);
  };

  if (isLoading) {
    return <div className="space-y-6">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Syncs</h1>
          <p className="text-sm text-muted-foreground mt-1">Cross-cluster copy jobs</p>
        </div>
        <Button onClick={() => setDialogOpen(true)}>Start sync</Button>
      </div>

      {jobs.length === 0 ? (
        <EmptyState
          icon="sync"
          title="No sync jobs yet"
          description="Create your first cross-cluster sync job to copy objects from one bucket to another."
          action={
            <Button onClick={() => setDialogOpen(true)}>Start sync</Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {jobs.map((job) => (
            <Card key={job.id}>
              <CardHeader>
                <CardTitle className="text-lg flex items-start justify-between">
                  <span>{job.mode.toUpperCase()}</span>
                  <span className={`text-sm font-normal ${getStateColor(job.state)}`}>
                    {job.state}
                  </span>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="text-sm space-y-2">
                  <div className="flex items-center gap-2">
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4 text-muted-foreground">
                      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                      <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
                      <line x1="12" y1="22.08" x2="12" y2="12"/>
                    </svg>
                    <span className="font-medium">{job.srcBucket}</span>
                  </div>
                  <div className="flex items-center gap-2 pl-6">
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4 text-muted-foreground">
                      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                      <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
                      <line x1="12" y1="22.08" x2="12" y2="12"/>
                    </svg>
                    <span className="font-medium">{job.dstBucket}</span>
                  </div>
                </div>

                {job.progress.objectsTotal > 0 && (
                  <div className="space-y-1">
                    <div className="flex justify-between text-xs text-muted-foreground">
                      <span>{job.progress.objectsCopied} / {job.progress.objectsTotal} objects</span>
                      <span>{getProgressPercent(job)}%</span>
                    </div>
                    <Progress value={getProgressPercent(job)} />
                  </div>
                )}

                {job.lastError && (
                  <p className="text-xs text-red-600">{job.lastError}</p>
                )}

                <div className="flex items-center gap-2 pt-2">
                  {(job.state === "running" || job.state === "queued") && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handlePause(job.id, job.state)}
                      disabled={pauseMutation.isPending}
                    >
                      Pause
                    </Button>
                  )}
                  {job.state === "paused" && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleResume(job.id)}
                      disabled={resumeMutation.isPending}
                    >
                      Resume
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDelete(job.id)}
                    disabled={deleteMutation.isPending}
                  >
                    Delete
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <StartSyncDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </div>
  );
}
