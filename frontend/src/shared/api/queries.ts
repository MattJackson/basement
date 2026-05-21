import { useMutation, useQuery } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/types.gen";

type Node = components["schemas"]["Node"];
type Layout = components["schemas"]["Layout"];
type Caps = components["schemas"]["Caps"];
type Bucket = components["schemas"]["Bucket"];
type Key = components["schemas"]["Key"];
type Connection = components["schemas"]["Connection"];

/**
 * apiError builds a user-presentable Error from a non-2xx response.
 * Uses the uniform error shape from design.md (`{error:{code,message,details}}`)
 * so screens can show the upstream cause instead of a generic message.
 */
function apiError(
  resource: string,
  status: number,
  body: unknown,
): Error {
  let code = `HTTP_${status}`;
  let message = `${resource} failed (HTTP ${status})`;
  if (body && typeof body === "object" && "error" in body) {
    const e = (body as { error?: { code?: string; message?: string } }).error;
    if (e?.code) code = e.code;
    if (e?.message) message = e.message;
  }
  const err = new Error(`${code}: ${message}`);
  (err as Error & { code?: string; status?: number }).code = code;
  (err as Error & { code?: string; status?: number }).status = status;
  return err;
}

/** Build-time version/commit/builtAt from /api/v1/version (public, no auth). */
export function useVersion() {
  return useQuery<{ version: string; commit: string; builtAt: string }>({
    queryKey: ["version"],
    queryFn: async () => {
      const res = await fetch("/api/v1/version");
      if (!res.ok) throw new Error(`version fetch ${res.status}`);
      return res.json();
    },
    staleTime: Infinity,
  });
}

export function useCapabilities() {
  return useQuery<Caps>({
    queryKey: ["capabilities"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/capabilities");
      if (!response.ok || !data) throw apiError("capabilities", response.status, error);
      return data as Caps;
    },
  });
}

export function useNodes(cid: string) {
  return useQuery<Node[]>({
    queryKey: ["admin", "clusters", cid, "nodes"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/nodes", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`admin/clusters/${cid}/nodes`, response.status, error);
      return data as Node[];
    },
    enabled: !!cid,
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
    retry: 1,
  });
}

export function useLayout(cid: string) {
  return useQuery<Layout>({
    queryKey: ["admin", "clusters", cid, "layout"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/layout", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`admin/clusters/${cid}/layout`, response.status, error);
      return data as Layout;
    },
    enabled: !!cid,
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
    retry: 1,
  });
}

export function useBuckets() {
  return useQuery<components["schemas"]["AggregatedBucketsResponse"]>({
    queryKey: ["admin", "buckets"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/buckets");
      if (!response.ok || !data) throw apiError("admin/buckets", response.status, error);
      return (data as unknown) as components["schemas"]["AggregatedBucketsResponse"];
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

// Per-cluster bucket list — directly hits /admin/clusters/{cid}/buckets
// rather than going through the aggregate fan-out. Used on the cluster
// detail page and for bucket-count badges on the clusters list.
export function useClusterBuckets(cid: string) {
  return useQuery<Bucket[]>({
    queryKey: ["admin", "clusters", cid, "buckets"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/buckets", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`admin/clusters/${cid}/buckets`, response.status, error);
      return data as Bucket[];
    },
    enabled: !!cid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useClusterKeys(cid: string) {
  return useQuery<Key[]>({
    queryKey: ["admin", "clusters", cid, "keys"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/keys", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`admin/clusters/${cid}/keys`, response.status, error);
      return data as Key[];
    },
    enabled: !!cid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useBucketsFlat() {
  const query = useBuckets();
  if (query.data) {
    return {
      ...query,
      data: query.data.buckets,
    };
  }
  return query;
}

export function useBucket(cid: string, id: string) {
  return useQuery<Bucket>({
    queryKey: ["admin", "clusters", cid, "buckets", id],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/buckets/{id}", {
        params: { path: { cid, id } },
      });
      if (!response.ok || !data) throw apiError(`admin/buckets/${id}`, response.status, error);
      return data as Bucket;
    },
    enabled: !!cid && !!id,
    staleTime: 30_000,
  });
}

export function useKeys() {
  return useQuery<components["schemas"]["AggregatedKeysResponse"]>({
    queryKey: ["admin", "keys"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/keys");
      if (!response.ok || !data) throw apiError("admin/keys", response.status, error);
      return (data as unknown) as components["schemas"]["AggregatedKeysResponse"];
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useKeysFlat() {
  const query = useKeys();
  if (query.data) {
    return {
      ...query,
      data: query.data.keys,
    };
  }
  return query;
}

export function useKey(cid: string, id: string) {
  return useQuery<Key>({
    queryKey: ["admin", "clusters", cid, "keys", id],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}/keys/{id}", {
        params: { path: { cid, id } },
      });
      if (!response.ok || !data) throw apiError(`admin/keys/${id}`, response.status, error);
      return data as Key;
    },
    enabled: !!cid && !!id,
    staleTime: 30_000,
  });
}

export function useListClusters() {
  return useQuery<Connection[]>({
    queryKey: ["admin", "clusters"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters");
      if (!response.ok || !data) throw apiError("admin/clusters", response.status, error);
      return (data as unknown[]) as Connection[];
    },
    staleTime: 60 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useGetCluster(cid: string) {
  return useQuery<Connection>({
    queryKey: ["admin", "clusters", cid],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/admin/clusters/{cid}", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`admin/clusters/${cid}`, response.status, error);
      return data as Connection;
    },
    enabled: !!cid,
    staleTime: 30_000,
  });
}

export function useTestClusterQuery(cid: string, opts: { auto?: boolean } = {}) {
  // Two modes:
  //   - auto=true: fires on mount + caches 60s (cluster detail page;
  //     operator complained "Connection test never runs, always says
  //     Unavailable"). 60s cache prevents page-flip thrashing.
  //   - auto=false (default): manual-only, caller .refetch()s. Used
  //     by the cluster list rows where N parallel /v1/health calls
  //     on Garage would saturate the server (10–20s each).
  return useQuery<components["schemas"]["ConnectionTestResult"], Error>({
    queryKey: ["admin", "clusters", cid, "_test"],
    queryFn: async () => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/_test", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`testCluster/${cid}`, response.status, error);
      return data as components["schemas"]["ConnectionTestResult"];
    },
    enabled: !!cid && (opts.auto ?? false),
    staleTime: 60 * 1000,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
    refetchOnReconnect: false,
  });
}

// User endpoints — filtered by grants (server-side).
export function useUserClusters() {
  return useQuery<Connection[]>({
    queryKey: ["user", "clusters"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/clusters");
      if (!response.ok || !data) throw apiError("user/clusters", response.status, error);
      return (data as unknown[]) as Connection[];
    },
    staleTime: 60 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useUserCluster(cid: string) {
  return useQuery<Connection>({
    queryKey: ["user", "clusters", cid],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/clusters/{cid}", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`user/clusters/${cid}`, response.status, error);
      return data as Connection;
    },
    enabled: !!cid,
    staleTime: 30_000,
  });
}

export function useUserClusterBuckets(cid: string) {
  return useQuery<Bucket[]>({
    queryKey: ["user", "clusters", cid, "buckets"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/clusters/{cid}/buckets", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`user/clusters/${cid}/buckets`, response.status, error);
      return data as Bucket[];
    },
    enabled: !!cid,
    staleTime: 30 * 1000,
  });
}

// Single-bucket detail (user persona). Garage v1's ListBuckets
// doesn't include bytes/objects, so the list view hydrates each row
// with its own GetBucket call — same pattern as the admin side.
export function useUserBucket(cid: string, bid: string) {
  return useQuery<Bucket>({
    queryKey: ["user", "clusters", cid, "buckets", bid],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/clusters/{cid}/buckets/{bid}", {
        params: { path: { cid, bid } },
      });
      if (!response.ok || !data) throw apiError(`user/buckets/${bid}`, response.status, error);
      return data as Bucket;
    },
    enabled: !!cid && !!bid,
    staleTime: 30_000,
    retry: 1,
  });
}

export function useUserKeys() {
  return useQuery<components["schemas"]["AggregatedUserKeysResponse"]>({
    queryKey: ["user", "keys"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/keys");
      if (!response.ok || !data) throw apiError("user/keys", response.status, error);
      return (data as unknown) as components["schemas"]["AggregatedUserKeysResponse"];
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useUserKeysFlat() {
  const query = useUserKeys();
  if (query.data) {
    return {
      ...query,
      data: query.data.keys,
    };
  }
  return query;
}

// v0.7.0d USER.OBJECTBROWSE — object browser hooks.
export function useUserObjects(cid: string | null, bid: string | null, prefix: string = "", token: string = "") {
  return useQuery<components["schemas"]["ObjectPage"]>({
    queryKey: ["user", "objects", cid, bid, prefix, token],
    queryFn: async () => {
      // Use native fetch — `client.GET(url as any, ...)` (the
      // openapi-fetch path) silently 404s when the URL isn't a
      // registered OpenAPI template path. The OpenAPI codegen
      // hasn't picked up the user-objects endpoint yet, so we go
      // around it. Switch back to client.GET once gen:api covers it.
      const params: Record<string, string> = {};
      if (prefix) params.prefix = prefix;
      if (token) params.token = token;
      const qs = new URLSearchParams(params).toString();
      const url = `/api/v1/user/clusters/${cid}/buckets/${bid}/objects${qs ? `?${qs}` : ""}`;

      const res = await fetch(url, { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/objects/${cid}/${bid}`, res.status, body);
      return body as components["schemas"]["ObjectPage"];
    },
    enabled: !!cid && !!bid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useUserPresignGet(cid: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, ttl }: { key: string; ttl: number }) => {
      if (!cid || !bid || !key) throw new Error("Missing required parameters");

      const params: Record<string, string> = {};
      params.ttl = String(ttl);

      let url = `/api/v1/user/clusters/${cid}/buckets/${bid}/objects/${encodeURIComponent(key)}/presign-get`;
      const qs = new URLSearchParams(params).toString();
      if (qs) url += `?${qs}`;

      const { data, error, response } = await client.POST(url as any, { params: { query: params } });
      if (!response.ok || !data) throw apiError(`user/presign-get/${cid}/${bid}/${key}`, response.status, error);
      return data as components["schemas"]["PresignedURL"];
    },
  });
}

// Upload mutation hooks for v0.7.0e USER.UPLOAD

export function useUserPresignPut(cid: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, contentType, ttl }: { key: string; contentType?: string; ttl?: number }) => {
      if (!cid || !bid || !key) throw new Error("Missing required parameters");

      const params: Record<string, string> = {};
      params.ttl = String(ttl ?? 3600);

      let url = `/api/v1/user/clusters/${cid}/buckets/${bid}/objects/${encodeURIComponent(key)}/presign-put`;
      const qs = new URLSearchParams(params).toString();
      if (qs) url += `?${qs}`;

      const { data, error, response } = await client.POST(url as any, { 
        params: { query: params },
        body: { contentType },
      });
      if (!response.ok || !data) throw apiError(`user/presign-put/${cid}/${bid}/${key}`, response.status, error);
      return data as components["schemas"]["PresignedURL"];
    },
  });
}

export function useUserMultipartInit(cid: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, contentType }: { key: string; contentType?: string }) => {
      if (!cid || !bid || !key) throw new Error("Missing required parameters");

      const { data, error, response } = await client.POST(
        "/api/v1/user/clusters/{cid}/buckets/{bid}/multipart/init" as any,
        { 
          params: { path: { cid, bid } },
          body: { key, contentType },
        }
      );
      if (!response.ok || !data) throw apiError(`user/multipart/init/${cid}/${bid}/${key}`, response.status, error);
      return data as components["schemas"]["MultipartUpload"];
    },
  });
}

export function useUserMultipartPartPresign(cid: string | null, bid: string | null, uploadId: string | null) {
  return useMutation({
    mutationFn: async ({ partNumber, ttl }: { partNumber: number; ttl?: number }) => {
      if (!cid || !bid || !uploadId) throw new Error("Missing required parameters");

      const params: Record<string, string> = {};
      params.ttl = String(ttl ?? 3600);

      let url = `/api/v1/user/clusters/${cid}/buckets/${bid}/multipart/${encodeURIComponent(uploadId)}/part/${partNumber}/presign`;
      const qs = new URLSearchParams(params).toString();
      if (qs) url += `?${qs}`;

      const { data, error, response } = await client.POST(url as any, { params: { query: params } });
      if (!response.ok || !data) throw apiError(`user/multipart/presign-part/${cid}/${bid}/${uploadId}/${partNumber}`, response.status, error);
      return data as components["schemas"]["PresignedURL"];
    },
  });
}

export function useUserMultipartComplete(cid: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ uploadId, parts }: { uploadId: string; parts: { partNumber: number; etag: string }[] }) => {
      if (!cid || !bid || !uploadId) throw new Error("Missing required parameters");

      const { data, error, response } = await client.POST(
        "/api/v1/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}/complete" as any,
        { 
          params: { path: { cid, bid, uploadId } },
          body: { parts },
        }
      );
      if (!response.ok) throw apiError(`user/multipart/complete/${cid}/${bid}/${uploadId}`, response.status, error);
      return data;
    },
  });
}

export function useUserMultipartAbort(cid: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async (uploadId: string) => {
      if (!cid || !bid || !uploadId) throw new Error("Missing required parameters");

      const { data, error, response } = await client.DELETE(
        "/api/v1/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}" as any,
        { 
          params: { path: { cid, bid, uploadId } },
        }
      );
      if (!response.ok) throw apiError(`user/multipart/abort/${cid}/${bid}/${uploadId}`, response.status, error);
      return data;
    },
  });
}

// Org capabilities visible to current user.
// Inlined type until OrgCapabilities schema lands in basement.yaml +
// types.gen.ts; the user-facing surface is intentionally narrow.
export interface UserVisibleOrgCapabilities {
  signupMode?: "closed" | "invite" | "open";
  enabledDrivers?: string[];
  allowUserBackends?: boolean;
  userBackendDrivers?: string[];
  oidcOnly?: boolean;
}

export function useOrgCapabilities() {
  return useQuery<UserVisibleOrgCapabilities>({
    queryKey: ["org-capabilities"],
    queryFn: async () => {
      const res = await fetch("/api/v1/auth/org-capabilities");
      if (!res.ok) throw new Error(`org-capabilities ${res.status}`);
      return res.json();
    },
  });
}

// v0.7.0g USER.SHARES — share link hooks.
export function useUserShares() {
  return useQuery<components["schemas"]["Share"][]>({
    queryKey: ["user", "shares"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/shares");
      if (!response.ok || !data) throw apiError("user/shares", response.status, error);
      return (data as unknown[]) as components["schemas"]["Share"][];
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useCreateUserShare() {
  return useMutation({
    mutationFn: async (data: {
      connectionId: string;
      bucketId: string;
      prefix?: string;
      key?: string;
      expiresAt?: string;
      downloadLimit?: number;
      password?: string;
    }) => {
      const { data: result, error, response } = await client.POST("/user/shares", { body: data });
      if (!response.ok || !result) throw apiError("user/shares/create", response.status, error);
      return result as components["schemas"]["Share"];
    },
  });
}

export function useRevokeUserShare() {
  return useMutation({
    mutationFn: async (token: string) => {
      const { data, error, response } = await client.DELETE("/user/shares/{token}", {
        params: { path: { token } },
      });
      if (!response.ok || !data) throw apiError(`user/shares/revoke/${token}`, response.status, error);
      return data;
    },
  });
}

// v0.7.0h SHARE.PUBLIC — public share hooks (no auth required).

export function useShareInfo(token: string | null) {
  return useQuery<components["schemas"]["ShareInfoResponse"] | null>({
    queryKey: ["share", token ?? "null"],
    queryFn: async () => {
      if (!token) throw new Error("Token required");

      const res = await fetch(`/api/v1/share/${encodeURIComponent(token)}/info`);
      if (!res.ok && res.status !== 404) {
        const body = await res.json().catch(() => ({}));
        throw apiError(`share/info/${token}`, res.status, body);
      }

      // Return null for not-found; screens handle their own rendering.
      if (!res.ok && res.status === 404) {
        return null;
      }

      const data = await res.json();
      return data as components["schemas"]["ShareInfoResponse"];
    },
    enabled: !!token,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useShareAuth() {
  return useMutation({
    mutationFn: async ({ token, password }: { token: string; password: string }) => {
      const res = await fetch(`/api/v1/share/${encodeURIComponent(token)}/auth`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password }),
      });

      if (!res.ok && res.status !== 200) {
        const errorBody = await res.json().catch(() => ({}));
        throw apiError(`share/auth/${token}`, res.status, errorBody);
      }

      return res.json();
    },
  });
}

export function useShareList(token: string | null, prefix: string = "") {
  return useQuery<components["schemas"]["ObjectPage"]>({
    queryKey: ["share", token, "list", prefix],
    queryFn: async () => {
      if (!token) throw new Error("Token required");

      const params: Record<string, string> = {};
      if (prefix) params.prefix = prefix;

      let url = `/api/v1/share/${encodeURIComponent(token)}/list`;
      const qs = new URLSearchParams(params).toString();
      if (qs) url += `?${qs}`;

      const res = await fetch(url);
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw apiError(`share/list/${token}`, res.status, body);
      }

      const data = await res.json();
      return data as components["schemas"]["ObjectPage"];
    },
    enabled: !!token && prefix !== "", // Only enable when we have a token and prefix
    staleTime: 30 * 1000,
    retry: 1,
  });
}

// v0.8.0c SYNC.ENGINE.PULL — sync job hooks.

export function useUserSyncs() {
  return useQuery<components["schemas"]["SyncJob"][]>({
    queryKey: ["user", "syncs"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/syncs");
      if (!response.ok || !data) throw apiError("user/syncs", response.status, error);
      return (data as unknown[]) as components["schemas"]["SyncJob"][];
    },
    staleTime: 5 * 1000, // Refresh every 5 seconds for active jobs
    refetchInterval: 2000, // Poll every 2s while job is running/queued
    retry: 1,
  });
}

export function useUserSync(id: string | null) {
  return useQuery<components["schemas"]["SyncJob"]>({
    queryKey: ["user", "syncs", id],
    queryFn: async () => {
      if (!id) throw new Error("Sync ID required");
      const { data, error, response } = await client.GET("/user/syncs/{id}", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/syncs/${id}`, response.status, error);
      return data as components["schemas"]["SyncJob"];
    },
    enabled: !!id,
    staleTime: 5 * 1000,
    refetchInterval: (query) => {
      // Poll every 2s while job is running or queued
      const state = query.state?.data?.state;
      return state === "running" || state === "queued" ? 2000 : false;
    },
    retry: 1,
  });
}

export function useCreateUserSync() {
  return useMutation({
    mutationFn: async (data: components["schemas"]["UserSyncCreateRequest"]) => {
      const { data: result, error, response } = await client.POST("/user/syncs", { body: data });
      if (!response.ok || !result) throw apiError("user/syncs/create", response.status, error);
      return result as components["schemas"]["CreateSyncResponse"];
    },
  });
}

export function useDeleteUserSync() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { data, error, response } = await client.DELETE("/user/syncs/{id}", {
        params: { path: { id } },
      });
      if (!response.ok) throw apiError(`user/syncs/delete/${id}`, response.status, error);
      return data;
    },
  });
}

export function usePauseUserSync() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { data, error, response } = await client.POST("/user/syncs/{id}/pause", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/syncs/pause/${id}`, response.status, error);
      return data as { state: string };
    },
  });
}

export function useResumeUserSync() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { data, error, response } = await client.POST("/user/syncs/{id}/resume", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/syncs/resume/${id}`, response.status, error);
      return data as { state: string };
    },
  });
}

// ADR-0001 v0.9.0g: policy matrix editor (/admin/policies).
//
// The OpenAPI spec doesn't carry these endpoints yet, so we go around
// openapi-fetch with bare fetch — same pattern as useOrgCapabilities.
// All mutations invalidate ["admin", "policies"] so the editor pane
// refreshes on every mutation.
export type PolicyCapability = { id: string; description: string };
export type PolicyRole = {
  id: string;
  label: string;
  description: string;
  capabilities: string[];
  seed: boolean;
};
export type PolicyAssignment = {
  userId: string;
  roleId: string;
  scope: string;
};
export type PoliciesResponse = {
  capabilities: PolicyCapability[];
  roles: PolicyRole[];
  assignments: PolicyAssignment[];
};

export function usePolicies() {
  return useQuery<PoliciesResponse>({
    queryKey: ["admin", "policies"],
    queryFn: async () => {
      const res = await fetch("/api/v1/admin/policies", { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/policies", res.status, body);
      return body as PoliciesResponse;
    },
    staleTime: 30 * 1000,
  });
}

export function useUpsertRole() {
  return useMutation({
    mutationFn: async (role: Omit<PolicyRole, "seed"> & { seed?: boolean }) => {
      const res = await fetch("/api/v1/admin/policies/roles", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(role),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`admin/policies/roles/upsert/${role.id}`, res.status, body);
      return body as PolicyRole;
    },
  });
}

export function useDeleteRole() {
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/api/v1/admin/policies/roles/${encodeURIComponent(id)}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`admin/policies/roles/delete/${id}`, res.status, body);
      }
      return null;
    },
  });
}

export function useAssignRole() {
  return useMutation({
    mutationFn: async (assignment: PolicyAssignment) => {
      const res = await fetch("/api/v1/admin/policies/assignments", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(assignment),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/policies/assignments/assign", res.status, body);
      return body as PolicyAssignment;
    },
  });
}

export function useUnassignRole() {
  return useMutation({
    mutationFn: async (a: PolicyAssignment) => {
      const params = new URLSearchParams({
        userId: a.userId,
        roleId: a.roleId,
        scope: a.scope,
      });
      const res = await fetch(`/api/v1/admin/policies/assignments?${params.toString()}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError("admin/policies/assignments/unassign", res.status, body);
      }
      return null;
    },
  });
}

