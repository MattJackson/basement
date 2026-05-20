import { createFileRoute } from "@tanstack/react-router";
import { useOrgCapabilities } from "@/shared/api/queries";
import { useCreateUserCluster, useTestUserCluster } from "@/shared/api/mutations";
import { useState } from "react";

export const Route = createFileRoute("/files/clusters/new")({
  component: AddClusterPage,
});

function AddClusterPage() {
  const { data: caps, isLoading: loadingCaps } = useOrgCapabilities();
  const createUserCluster = useCreateUserCluster();
  const testUserCluster = useTestUserCluster();

  const [label, setLabel] = useState("");
  const [driver, setDriver] = useState(caps?.userBackendDrivers[0] || "");
  const [config, setConfig] = useState<Record<string, string>>({});
  const [color, setColor] = useState("#123456");
  const [testResult, setTestResult] = useState<"idle" | "testing" | "success" | "error">("idle");
  const [testMessage, setTestMessage] = useState("");

  if (loadingCaps) {
    return <div className="p-4">Loading...</div>;
  }

  if (!caps?.allowUserBackends) {
    return (
      <div className="space-y-6">
        <header>
          <a href="/files" className="text-sm text-muted-foreground hover:underline">
            ← Back to clusters
          </a>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight mt-2">Add cluster</h1>
        </header>
        <div className="rounded-lg border bg-card p-6 text-center">
          <p className="text-muted-foreground">
            Adding user clusters is disabled by your administrator. Please contact them to request access.
          </p>
        </div>
      </div>
    );
  }

  const availableDrivers = caps.userBackendDrivers;

  const handleTestConnection = async () => {
    setTestResult("testing");
    try {
      await testUserCluster.mutateAsync({ driver, config });
      setTestResult("success");
      setTestMessage("Connection successful!");
    } catch (err: any) {
      setTestResult("error");
      setTestMessage(err.message || "Connection failed");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!label || !driver || Object.keys(config).length === 0) return;

    try {
      await createUserCluster.mutateAsync({ label, driver, config, color });
    } catch (err) {
      console.error("Failed to create cluster:", err);
    }
  };

  const isFormValid = label.trim() && driver && Object.keys(config).length > 0;
  const canSubmit = isFormValid && (testResult === "success" || testResult === "idle");

  return (
    <div className="space-y-6">
      <header>
        <a href="/files" className="text-sm text-muted-foreground hover:underline">
          ← Back to clusters
        </a>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight mt-2">Add cluster</h1>
        <p className="text-sm text-muted-foreground mt-1">Register your own storage backend</p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <label htmlFor="label" className="text-sm font-medium">
              Label *
            </label>
            <input
              id="label"
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="My Work Cluster"
              className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div className="space-y-2">
            <label htmlFor="color" className="text-sm font-medium">
              Color
            </label>
            <input
              id="color"
              type="color"
              value={color}
              onChange={(e) => setColor(e.target.value)}
              className="h-10 w-full rounded-md border bg-background px-1 py-1 cursor-pointer"
            />
          </div>

          <div className="space-y-2">
            <label htmlFor="driver" className="text-sm font-medium">
              Driver *
            </label>
            <select
              id="driver"
              value={driver}
              onChange={(e) => setDriver(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            >
              {availableDrivers.map((d: string) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="space-y-2">
          <label className="text-sm font-medium">Configuration *</label>
          <div className="grid gap-2 md:grid-cols-2">
            {driver === "garage" || driver === "garage-v1" ? (
              <>
                <input
                  type="text"
                  placeholder="admin_url"
                  value={config.admin_url || ""}
                  onChange={(e) => setConfig({ ...config, admin_url: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
                <input
                  type="text"
                  placeholder="admin_token (optional)"
                  value={config.admin_token || ""}
                  onChange={(e) => setConfig({ ...config, admin_token: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
              </>
            ) : driver === "aws-s3" ? (
              <>
                <input
                  type="text"
                  placeholder="region"
                  value={config.region || ""}
                  onChange={(e) => setConfig({ ...config, region: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
                <input
                  type="text"
                  placeholder="access_key_id (optional)"
                  value={config.access_key_id || ""}
                  onChange={(e) => setConfig({ ...config, access_key_id: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
              </>
            ) : driver === "minio" ? (
              <>
                <input
                  type="text"
                  placeholder="endpoint"
                  value={config.endpoint || ""}
                  onChange={(e) => setConfig({ ...config, endpoint: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
                <input
                  type="text"
                  placeholder="access_key (optional)"
                  value={config.access_key || ""}
                  onChange={(e) => setConfig({ ...config, access_key: e.target.value })}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
              </>
            ) : null}
          </div>
        </div>

        <div className="flex items-center gap-4">
          <button
            type="button"
            onClick={handleTestConnection}
            disabled={!isFormValid || testResult === "testing"}
            className="rounded-md bg-secondary px-4 py-2 text-sm font-medium hover:bg-secondary/80 disabled:opacity-50"
          >
            {testResult === "testing" ? "Testing..." : "Test connection"}
          </button>

          {testResult !== "idle" && (
            <div className={`text-sm ${testResult === "success" ? "text-green-600" : testResult === "error" ? "text-red-600" : ""}`}>
              {testMessage}
            </div>
          )}
        </div>

        <button
          type="submit"
          disabled={!canSubmit || createUserCluster.isPending}
          className="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {createUserCluster.isPending ? "Creating..." : "Add cluster"}
        </button>
      </form>
    </div>
  );
}

export default AddClusterPage;
