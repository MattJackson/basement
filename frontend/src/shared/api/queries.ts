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

// ADR-0002 v1.1.0c: region-tier user hooks. The user persona thinks
// in regions now (an S3 endpoint + key the user owns); these hooks
// hit /api/v1/user/regions/* directly. We bypass openapi-fetch and
// use native fetch (matches the pattern used for ad-hoc routes) because the
// OpenAPI spec hasn't been regenerated for the region endpoints yet.

export interface UserRegion {
  id: string;
  userId: string;
  alias: string;
  endpoint: string;
  region: string;
  accessKeyId: string;
  createdAt: string;
  updatedAt: string;
  lastUsedAt?: string;
}

export function useUserRegions() {
  return useQuery<UserRegion[]>({
    queryKey: ["user", "regions"],
    queryFn: async () => {
      const res = await fetch("/api/v1/user/regions", { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("user/regions", res.status, body);
      return (body as UserRegion[]) ?? [];
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

export function useUserRegion(id: string | null) {
  return useQuery<UserRegion>({
    queryKey: ["user", "regions", id],
    queryFn: async () => {
      if (!id) throw new Error("Region id required");
      const res = await fetch(`/api/v1/user/regions/${encodeURIComponent(id)}`, {
        credentials: "include",
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${id}`, res.status, body);
      return body as UserRegion;
    },
    enabled: !!id,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useUserRegionBuckets(regionId: string | null) {
  return useQuery<Bucket[]>({
    queryKey: ["user", "regions", regionId, "buckets"],
    queryFn: async () => {
      if (!regionId) throw new Error("Region id required");
      const res = await fetch(
        `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets`,
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/buckets`, res.status, body);
      return (body as Bucket[]) ?? [];
    },
    enabled: !!regionId,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export function useUserRegionObjects(
  regionId: string | null,
  bid: string | null,
  prefix: string = "",
  token: string = "",
) {
  return useQuery<components["schemas"]["ObjectPage"]>({
    queryKey: ["user", "regions", regionId, "buckets", bid, "objects", prefix, token],
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (prefix) params.prefix = prefix;
      if (token) params.token = token;
      const qs = new URLSearchParams(params).toString();
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId!)}/buckets/${encodeURIComponent(bid!)}/objects${qs ? `?${qs}` : ""}`;
      const res = await fetch(url, { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/buckets/${bid}/objects`, res.status, body);
      return body as components["schemas"]["ObjectPage"];
    },
    enabled: !!regionId && !!bid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

export interface CreateUserRegionRequest {
  alias: string;
  endpoint: string;
  accessKeyId: string;
  secretKey: string;
  region?: string;
}

export function useCreateUserRegion() {
  return useMutation<UserRegion, Error, CreateUserRegionRequest>({
    mutationFn: async (input) => {
      const res = await fetch("/api/v1/user/regions", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("user/regions/create", res.status, body);
      return body as UserRegion;
    },
  });
}

export function useDeleteUserRegion() {
  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      const res = await fetch(`/api/v1/user/regions/${encodeURIComponent(id)}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`user/regions/delete/${id}`, res.status, body);
      }
    },
  });
}

export function useUserRegionPresignGet(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, ttl }: { key: string; ttl?: number }) => {
      if (!regionId || !bid || !key) throw new Error("Missing required parameters");
      const params = new URLSearchParams({ ttl: String(ttl ?? 3600) });
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/objects/${encodeURIComponent(key)}/presign-get?${params.toString()}`;
      // Backend route is registered as GET in v1.1.0b; native fetch keeps
      // us off the OpenAPI template that doesn't yet know the route.
      const res = await fetch(url, { method: "GET", credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/presign-get/${key}`, res.status, body);
      return body as components["schemas"]["PresignedURL"];
    },
  });
}

export function useUserRegionPresignPut(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, contentType, ttl }: { key: string; contentType?: string; ttl?: number }) => {
      if (!regionId || !bid || !key) throw new Error("Missing required parameters");
      const params = new URLSearchParams({ ttl: String(ttl ?? 3600) });
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/objects/${encodeURIComponent(key)}/presign-put?${params.toString()}`;
      const res = await fetch(url, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ contentType }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/presign-put/${key}`, res.status, body);
      return body as components["schemas"]["PresignedURL"];
    },
  });
}

export function useUserRegionMultipartInit(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ key, contentType }: { key: string; contentType?: string }) => {
      if (!regionId || !bid || !key) throw new Error("Missing required parameters");
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/multipart/init`;
      const res = await fetch(url, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ key, contentType }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/multipart/init/${key}`, res.status, body);
      return body as components["schemas"]["MultipartUpload"];
    },
  });
}

export function useUserRegionMultipartPartPresign(
  regionId: string | null,
  bid: string | null,
  uploadId: string | null,
) {
  return useMutation({
    mutationFn: async ({ partNumber, ttl }: { partNumber: number; ttl?: number }) => {
      if (!regionId || !bid || !uploadId) throw new Error("Missing required parameters");
      const params = new URLSearchParams({ ttl: String(ttl ?? 3600) });
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/multipart/${encodeURIComponent(uploadId)}/part/${partNumber}/presign?${params.toString()}`;
      const res = await fetch(url, { method: "POST", credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/multipart/${uploadId}/part/${partNumber}`, res.status, body);
      return body as components["schemas"]["PresignedURL"];
    },
  });
}

export function useUserRegionMultipartComplete(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async ({ uploadId, parts }: { uploadId: string; parts: { partNumber: number; etag: string }[] }) => {
      if (!regionId || !bid || !uploadId) throw new Error("Missing required parameters");
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/multipart/${encodeURIComponent(uploadId)}/complete`;
      const res = await fetch(url, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ parts }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`user/regions/${regionId}/multipart/complete/${uploadId}`, res.status, body);
      }
    },
  });
}

export function useUserRegionMultipartAbort(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async (uploadId: string) => {
      if (!regionId || !bid || !uploadId) throw new Error("Missing required parameters");
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/multipart/${encodeURIComponent(uploadId)}`;
      const res = await fetch(url, { method: "DELETE", credentials: "include" });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`user/regions/${regionId}/multipart/abort/${uploadId}`, res.status, body);
      }
    },
  });
}

export function useDeleteUserRegionObject(regionId: string | null, bid: string | null) {
  return useMutation({
    mutationFn: async (key: string) => {
      if (!regionId || !bid || !key) throw new Error("Missing required parameters");
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/objects/${encodeURIComponent(key)}`;
      const res = await fetch(url, { method: "DELETE", credentials: "include" });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`user/regions/${regionId}/objects/delete/${key}`, res.status, body);
      }
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

// POLICY.SIM v0.9.0j — what-if simulator over the existing matrix.
//
// Pure analysis: no side effects, no enforcer mutation. The hook is a
// mutation rather than a query because operators want explicit "run
// this what-if" semantics — auto-fetching on field change would make
// every keystroke fire a request.
export type PolicyReasoningStep = { step: string; detail: string };
export type SimulateRequest = {
  userId: string;
  capability: string;
  scope: string;
};
export type SimulateResponse = {
  allowed: boolean;
  reasoning: PolicyReasoningStep[];
  matchingAssignments: PolicyAssignment[];
};

export function useSimulatePolicy() {
  return useMutation({
    mutationFn: async (req: SimulateRequest) => {
      const res = await fetch("/api/v1/admin/policies/simulate", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/policies/simulate", res.status, body);
      return body as SimulateResponse;
    },
  });
}

// v0.9.0i LIFECYCLE.WIZARD — bucket lifecycle policy hooks.
//
// The OpenAPI spec doesn't carry these endpoints yet, so we go around
// openapi-fetch with bare fetch — same pattern as the v0.9.0g policies
// and v0.9.0h migrations hooks. Hand-typed shapes match the backend's
// `internal/driver/driver.go` LifecycleRule + LifecycleCapabilities
// structs verbatim so the wire is round-trip-safe.
export interface LifecycleRule {
  id: string;
  status: "Enabled" | "Disabled";
  prefix?: string;
  expirationDays?: number;
  transitionDays?: number;
  transitionTier?: string;
  noncurrentDays?: number;
  abortMultipartDays?: number;
}

export interface LifecycleCapabilities {
  supported: boolean;
  expiration: boolean;
  transition: boolean;
  transitionTiers: string[];
  noncurrentDays: boolean;
  abortMultipartDays: boolean;
}

export interface LifecycleResponse {
  rules: LifecycleRule[];
  capabilities: LifecycleCapabilities;
}

export function useBucketLifecycle(cid: string, bid: string) {
  return useQuery<LifecycleResponse>({
    queryKey: ["admin", "clusters", cid, "buckets", bid, "lifecycle"],
    queryFn: async () => {
      const url = `/api/v1/admin/clusters/${encodeURIComponent(cid)}/buckets/${encodeURIComponent(bid)}/lifecycle`;
      const res = await fetch(url, { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`bucket-lifecycle/${cid}/${bid}`, res.status, body);
      // Backend always returns capabilities + rules (even on unsupported
      // drivers — rules is [] in that case).
      return body as LifecycleResponse;
    },
    enabled: !!cid && !!bid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

// usePutBucketLifecycle replaces the bucket's lifecycle policy. Pass
// an empty rules array to clear the policy (backend translates to
// DeleteBucketLifecycle on S3, empty UpdateBucket on Garage).
export function usePutBucketLifecycle(cid: string, bid: string) {
  return useMutation<LifecycleResponse, Error, { rules: LifecycleRule[] }>({
    mutationFn: async ({ rules }) => {
      const url = `/api/v1/admin/clusters/${encodeURIComponent(cid)}/buckets/${encodeURIComponent(bid)}/lifecycle`;
      const res = await fetch(url, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rules }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`bucket-lifecycle-put/${cid}/${bid}`, res.status, body);
      return body as LifecycleResponse;
    },
  });
}

// OBS.USAGE v0.9.0k — storage overview snapshot.
//
// Pure read; the backend aggregates from existing per-cluster reads
// (no metrics store, no time series — that's a v1.x decision). One
// big-number-card payload plus two top-N tables. Refetch on a 60s
// timer so the dashboard reflects real-world activity without
// hammering the backend's per-cluster fan-out.
export interface UsageTotals {
  clusters: number;
  buckets: number;
  keys: number;
  bytes: number;
  objects: number;
  grants: number;
}

export interface UsagePerCluster {
  id: string;
  label: string;
  bytes: number;
  objects: number;
  buckets: number;
  keys: number;
  healthy: boolean;
  error?: string;
}

export interface UsageTopBucket {
  clusterId: string;
  clusterLabel: string;
  bucketId: string;
  bucketAlias: string;
  bytes: number;
  objects: number;
}

export interface UsageOverviewResponse {
  totals: UsageTotals;
  perCluster: UsagePerCluster[];
  topBucketsByBytes: UsageTopBucket[];
  topBucketsByObjects: UsageTopBucket[];
}

export function useUsageOverview() {
  return useQuery<UsageOverviewResponse>({
    queryKey: ["admin", "usage", "overview"],
    queryFn: async () => {
      const res = await fetch("/api/v1/admin/usage/overview", {
        credentials: "include",
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/usage/overview", res.status, body);
      return body as UsageOverviewResponse;
    },
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
    retry: 1,
  });
}

// v1.0.0d: per-bucket time-series, populated hourly by the backend
// snapshot scheduler. Used by the inline trend chart on /admin/usage
// when a row is expanded. Default range 7 days; backend clamps to 90d.
export interface UsageSeriesPoint {
  time: string;
  bytes: number;
  objects: number;
}

export interface UsageSeriesResponse {
  snapshots: UsageSeriesPoint[];
  bucketAlias?: string;
  range: string;
}

export function useUsageSeries(cid: string, bid: string, enabled = true) {
  return useQuery<UsageSeriesResponse>({
    queryKey: ["admin", "usage", "series", cid, bid],
    queryFn: async () => {
      const params = new URLSearchParams({ cid, bid });
      const res = await fetch(
        "/api/v1/admin/usage/series?" + params.toString(),
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/usage/series", res.status, body);
      return body as UsageSeriesResponse;
    },
    enabled: enabled && !!cid && !!bid,
    staleTime: 60 * 1000,
    retry: 1,
  });
}

// v1.0.0c: audit log query. Filters mirror the backend QueryFilter
// shape; the response carries the events plus a `truncated` hint so
// the UI can render an honest "load more / narrow the window" CTA.

export interface AuditEvent {
  time: string;
  actor: string;
  actorRole?: string;
  action: string;
  resource: string;
  result: "success" | "failure";
  detail?: string;
  ip?: string;
  userAgent?: string;
}

export interface AuditFilter {
  from?: string;
  to?: string;
  actor?: string;
  action?: string;
  resource?: string;
  result?: string;
  limit?: number;
}

export interface AuditResponse {
  events: AuditEvent[];
  total: number;
  truncated: boolean;
}

export function useAudit(filter: AuditFilter) {
  return useQuery<AuditResponse>({
    queryKey: ["admin", "audit", filter],
    queryFn: async () => {
      const params = new URLSearchParams();
      for (const [k, v] of Object.entries(filter)) {
        if (v !== undefined && v !== "" && v !== null) {
          params.set(k, String(v));
        }
      }
      const url = "/api/v1/admin/audit" + (params.toString() ? "?" + params.toString() : "");
      const res = await fetch(url, { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/audit", res.status, body);
      return body as AuditResponse;
    },
    staleTime: 10 * 1000,
  });
}


