import { useState, useEffect } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "sonner";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/system")({
  component: adminPage(OrgCapabilitiesPage),
});

type OrgCapabilities = {
  signupMode?: "closed" | "invite" | "open";
  enabledDrivers?: string[];
  allowUserBackends?: boolean;
  userBackendDrivers?: string[];
  oidcOnly?: boolean;
};

function OrgCapabilitiesPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<OrgCapabilities | null>(null);

  useEffect(() => {
    fetchCapabilities();
  }, []);

  async function fetchCapabilities() {
    try {
      // @ts-ignore - API types not generated yet
      const { data: caps } = await client.GET("/admin/system", {});
      if (caps) {
        setData(caps as OrgCapabilities);
      }
    } catch (error) {
      toast.error("Failed to load system capabilities");
      navigate({ to: "/files" });
    } finally {
      setLoading(false);
    }
  }

  async function handleSave() {
    if (!data) return;

    try {
      // @ts-ignore - API types not generated yet
      await client.PATCH("/admin/system", {
        body: data,
      });
      toast.success("System capabilities updated");
    } catch (error) {
      toast.error("Failed to update system capabilities");
    }
  }

  if (loading) {
    return <div className="flex items-center justify-center min-h-[400px]">Loading...</div>;
  }

  if (!data) {
    return null;
  }

  function setEnabledDrivers(newEnabled: string[]) {
    // @ts-ignore - type compatibility issue with optional fields
    setData({ enabledDrivers: newEnabled });
  }

  function setUserBackendDrivers(newUserDrivers: string[]) {
    // @ts-ignore - type compatibility issue with optional fields
    setData({ userBackendDrivers: newUserDrivers });
  }

  const AVAILABLE_DRIVERS = ["garage", "garage-v1", "aws-s3", "minio"];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">System Configuration</h1>
        <p className="text-muted-foreground mt-1">
          Configure org-level capabilities and feature flags.
        </p>
      </div>

      <div className="space-y-4">
        {/* Signup Mode */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Signup Mode</label>
          <select
            value={data.signupMode ?? "invite"}
            onChange={(e) => setData({ ...data, signupMode: e.target.value as OrgCapabilities["signupMode"] })}
            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="closed">Closed (admin only)</option>
            <option value="invite">Invite only</option>
            <option value="open">Open (anyone can sign up)</option>
          </select>
        </div>

        {/* Enabled Drivers */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Enabled Drivers</label>
          <div className="flex flex-wrap gap-2">
            {AVAILABLE_DRIVERS.map((driver) => (
              <button
                key={driver}
                onClick={() => {
                  const currentDrivers = data.enabledDrivers ?? [];
                  const newEnabled = currentDrivers.includes(driver)
                    ? currentDrivers.filter((d) => d !== driver)
                    : [...currentDrivers, driver];
                  setEnabledDrivers(newEnabled);
                }}
                className={`px-3 py-1.5 rounded-md text-sm font-medium border ${
                  (data.enabledDrivers ?? []).includes(driver)
                    ? "bg-primary text-primary-foreground border-primary"
                    : "bg-background text-muted-foreground border-input"
                }`}
              >
                {driver}
              </button>
            ))}
          </div>
        </div>

        {/* Allow User Backends */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <input
              type="checkbox"
              checked={data.allowUserBackends ?? false}
              onChange={(e) => setData({ ...data, allowUserBackends: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300"
            />
            Allow User Backends
          </label>
        </div>

        {/* User Backend Drivers */}
        {data.allowUserBackends && (
          <div className="space-y-2">
            <label className="text-sm font-medium">User Backend Drivers</label>
            <div className="flex flex-wrap gap-2">
              {(data.enabledDrivers ?? []).map((driver) => (
                <button
                  key={driver}
                  onClick={() => {
                    const currentUsers = data.userBackendDrivers ?? [];
                    const newUserDrivers = currentUsers.includes(driver)
                      ? currentUsers.filter((d) => d !== driver)
                      : [...currentUsers, driver];
                    setUserBackendDrivers(newUserDrivers);
                  }}
                  className={`px-3 py-1.5 rounded-md text-sm font-medium border ${
                    (data.userBackendDrivers ?? []).includes(driver)
                      ? "bg-primary text-primary-foreground border-primary"
                      : "bg-background text-muted-foreground border-input"
                  }`}
                >
                  {driver}
                </button>
              ))}
            </div>
          </div>
        )}

        {/* OIDC Only */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <input
              type="checkbox"
              checked={data.oidcOnly ?? false}
              onChange={(e) => setData({ ...data, oidcOnly: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300"
            />
            OIDC Only Mode (hide local password login)
          </label>
        </div>

        <Button onClick={handleSave}>Save Changes</Button>
      </div>

      {/* Usage overview link card — OBS.USAGE v0.9.0k surfaces the */}
      {/* storage snapshot from the System page so the operator's */}
      {/* "configure the deployment" landing pad also points at the */}
      {/* "what is the deployment doing right now" view. */}
      <Card>
        <CardHeader>
          <CardTitle>Usage Overview</CardTitle>
          <CardDescription>
            Snapshot of storage usage across every cluster: totals, per-cluster
            breakdown, and the top buckets by size and object count.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link
            to="/admin/usage"
            className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            Open the usage overview &rarr;
          </Link>
        </CardContent>
      </Card>

      {/* Policies link card — ADR-0001 v0.9.0g surfaces the matrix */}
      {/* editor from the System page too, not just the persona menu. */}
      <Card>
        <CardHeader>
          <CardTitle>Policies</CardTitle>
          <CardDescription>
            Roles, capabilities, and per-user assignments. The matrix editor
            controls who can do what, at which scope.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link
            to="/admin/policies"
            className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            Open the policy matrix editor &rarr;
          </Link>
        </CardContent>
      </Card>
    </div>
  );
}
