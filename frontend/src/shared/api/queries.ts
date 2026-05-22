import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
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
 *
 * `details` is forwarded onto the Error object so screens that need a
 * structured payload (e.g. NO_ADMIN_BRIDGE surfacing the offending
 * endpoint + field) can read it without re-fetching.
 */
function apiError(
  resource: string,
  status: number,
  body: unknown,
): Error {
  let code = `HTTP_${status}`;
  let message = `${resource} failed (HTTP ${status})`;
  let details: Record<string, unknown> | undefined;
  if (body && typeof body === "object" && "error" in body) {
    const e = (body as { error?: { code?: string; message?: string; details?: Record<string, unknown> } }).error;
    if (e?.code) code = e.code;
    if (e?.message) message = e.message;
    if (e?.details) details = e.details;
  }
  const err = new Error(`${code}: ${message}`);
  (err as Error & { code?: string; status?: number; details?: Record<string, unknown> }).code = code;
  (err as Error & { code?: string; status?: number; details?: Record<string, unknown> }).status = status;
  if (details) {
    (err as Error & { code?: string; status?: number; details?: Record<string, unknown> }).details = details;
  }
  return err;
}

// v1.3.0b — per-driver endpoint placeholder + hint catalogue served
// from GET /api/v1/system/driver-defaults. Public (no auth) and the
// payload is static for a given binary, so we cache forever — the
// only refresh path is a page reload after a deploy.
export interface DriverDefaults {
  driver: string;
  displayName: string;
  adminUrl: string;
  adminUrlHint: string;
  s3Endpoint: string;
  s3EndpointHint: string;
  regionLabel: string;
  secretUrl?: string;
}

export function useDriverDefaults() {
  return useQuery<DriverDefaults[]>({
    queryKey: ["system", "driver-defaults"],
    queryFn: async () => {
      const res = await fetch("/api/v1/system/driver-defaults");
      if (!res.ok) throw new Error(`driver-defaults fetch ${res.status}`);
      return (await res.json()) as DriverDefaults[];
    },
    staleTime: Infinity,
    gcTime: Infinity,
  });
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
  // v1.3.0c: per-region S3 addressing toggle. "path" (the default for
  // every UserRegion shipped before this cycle) addresses requests as
  // endpoint/bucket/key; "virtual_host" routes as bucket.endpoint/key.
  // The server's wire format always populates this field after read
  // (via store.applyReadDefaults), so the FE can render the
  // per-card "via path-style" / "via virtual-host" subtitle without
  // null-checking.
  addressingStyle: "path" | "virtual_host";
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

// v1.4.0a: the wire response now wraps the bucket list in a small
// envelope so the FE can read a per-driver capability flag alongside
// the list. perBucketStatsAvailable is false on Garage v1 (and any
// future driver that can't surface Objects + Bytes via ListBuckets);
// the bucket-list page uses it to hide the Size + Objects columns
// entirely rather than render rows of em-dashes.
export interface UserRegionBucketList {
  buckets: Bucket[];
  perBucketStatsAvailable: boolean;
}

export function useUserRegionBuckets(regionId: string | null) {
  return useQuery<UserRegionBucketList>({
    queryKey: ["user", "regions", regionId, "buckets"],
    queryFn: async () => {
      if (!regionId) throw new Error("Region id required");
      const res = await fetch(
        `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets`,
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/buckets`, res.status, body);
      return {
        buckets: (body?.buckets as Bucket[]) ?? [],
        perBucketStatsAvailable: Boolean(body?.perBucketStatsAvailable),
      };
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

// v1.4.0a: paginated bucket browsing. Walks continuation tokens
// while staying inside a single prefix, accumulating pages so the
// virtualized object table can render the merged objects+folders
// list as one contiguous scrollable stream. Folders (commonPrefixes)
// only ever come back on the first page so we deliberately collect
// them once and ignore the empty arrays on subsequent pages.
export function useUserRegionObjectsInfinite(
  regionId: string | null,
  bid: string | null,
  prefix: string = "",
) {
  return useInfiniteQuery<components["schemas"]["ObjectPage"]>({
    queryKey: ["user", "regions", regionId, "buckets", bid, "objects-inf", prefix],
    initialPageParam: "" as string,
    queryFn: async ({ pageParam }) => {
      const params: Record<string, string> = {};
      if (prefix) params.prefix = prefix;
      if (pageParam) params.token = pageParam as string;
      const qs = new URLSearchParams(params).toString();
      const url = `/api/v1/user/regions/${encodeURIComponent(regionId!)}/buckets/${encodeURIComponent(bid!)}/objects${qs ? `?${qs}` : ""}`;
      const res = await fetch(url, { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/${regionId}/buckets/${bid}/objects`, res.status, body);
      return body as components["schemas"]["ObjectPage"];
    },
    getNextPageParam: (lastPage) => {
      if (lastPage?.isTruncated && lastPage?.nextContinuation) {
        return lastPage.nextContinuation;
      }
      return undefined;
    },
    enabled: !!regionId && !!bid,
    staleTime: 30 * 1000,
    retry: 1,
  });
}

// v1.2.0d: renamed from CreateUserRegionRequest. The user-facing
// concept is a KEY (each access key is the primary noun on the
// keychain); the storage tuple is still called UserRegion server-side.
export interface CreateUserKeyRequest {
  alias: string;
  endpoint: string;
  accessKeyId: string;
  secretKey: string;
  region?: string;
  // v1.3.0c: optional per-region addressing toggle. Omitted / undefined
  // sends nothing and the backend defaults to "path" for compatibility
  // with every UserRegion shipped before this cycle.
  addressingStyle?: "path" | "virtual_host";
}

// v1.2.0d: renamed from useCreateUserRegion. Still POSTs to
// /api/v1/user/regions because the server type is unchanged — only
// the FE label moved to match the operator's "key-first" mental model
// (a user may add multiple keys against the same endpoint).
export function useCreateUserKey() {
  return useMutation<UserRegion, Error, CreateUserKeyRequest>({
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

// v1.3.0d: bulk-create N UserRegions in one request. Each row is
// validated server-side independently; a per-row failure (duplicate
// alias, malformed endpoint) lands in `errors` while the rest of the
// batch creates normally. Used by /files/keys/new's "Bulk import"
// toggle to onboard multiple credentials in one paste.
export interface BulkCreateUserKeyRow {
  alias: string;
  endpoint: string;
  accessKeyId: string;
  secretKey: string;
  region?: string;
  addressingStyle?: "path" | "virtual_host";
}

export interface BulkCreateUserKeyResponse {
  created: UserRegion[];
  errors: Array<{
    index: number;
    alias?: string;
    endpoint?: string;
    error: string;
    message: string;
  }>;
}

export function useBulkCreateUserKeys() {
  return useMutation<BulkCreateUserKeyResponse, Error, BulkCreateUserKeyRow[]>({
    mutationFn: async (rows) => {
      const res = await fetch("/api/v1/user/regions/bulk", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ regions: rows }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("user/regions/bulk", res.status, body);
      return body as BulkCreateUserKeyResponse;
    },
  });
}

// v1.3.0c: rotate the access key + secret on an existing UserRegion
// in place. Alias / endpoint / region / addressingStyle / lastUsedAt
// history all survive — only the credential pair flips. Server-side
// audits as region:rotate and invalidates the cached S3 client so the
// next ListBuckets uses the fresh secret.
export interface RotateUserRegionRequest {
  regionId: string;
  accessKeyId: string;
  secretKey: string;
}

export function useRotateUserRegion() {
  return useMutation<UserRegion, Error, RotateUserRegionRequest>({
    mutationFn: async ({ regionId, accessKeyId, secretKey }) => {
      const res = await fetch(
        `/api/v1/user/regions/${encodeURIComponent(regionId)}/rotate`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ accessKeyId, secretKey }),
        },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/regions/rotate/${regionId}`, res.status, body);
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

// v1.5.0a BACKUP.SCHEDULED — scheduled bucket-to-bucket backup hooks.
// The wire types come from the generated openapi types, but we
// pre-narrow them here so callers don't have to traffic in optional
// chains for fields the server always returns.

export type BackupResult = components["schemas"]["BackupResult"];
export type Backup = components["schemas"]["Backup"];
export type UserBackupCreateRequest = components["schemas"]["UserBackupCreateRequest"];
// v1.5.0b additions — snapshot mode + retention policy.
export type RetentionPolicy = components["schemas"]["RetentionPolicy"];
export type BackupSnapshotEntry = components["schemas"]["BackupSnapshotEntry"];
export type BackupMode = "mirror" | "snapshot";

export function useUserBackups() {
  return useQuery<Backup[]>({
    queryKey: ["user", "backups"],
    queryFn: async () => {
      const { data, error, response } = await client.GET("/user/backups");
      if (!response.ok || !data) throw apiError("user/backups", response.status, error);
      return data as Backup[];
    },
    staleTime: 10_000,
    // Light polling so the list view sees lastResult updates within
    // ~10s of a scheduled run firing in the background.
    refetchInterval: 10_000,
    retry: 1,
  });
}

export function useUserBackup(id: string | null) {
  return useQuery<Backup>({
    queryKey: ["user", "backups", id],
    queryFn: async () => {
      if (!id) throw new Error("Backup ID required");
      const { data, error, response } = await client.GET("/user/backups/{id}", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/backups/${id}`, response.status, error);
      return data as Backup;
    },
    enabled: !!id,
    staleTime: 5_000,
    refetchInterval: 5_000,
    retry: 1,
  });
}

export function useCreateUserBackup() {
  return useMutation({
    mutationFn: async (body: UserBackupCreateRequest) => {
      const { data, error, response } = await client.POST("/user/backups", { body });
      if (!response.ok || !data) throw apiError("user/backups/create", response.status, error);
      return data as Backup;
    },
  });
}

export function useUpdateUserBackup() {
  return useMutation({
    mutationFn: async (args: { id: string; body: UserBackupCreateRequest }) => {
      const { data, error, response } = await client.PUT("/user/backups/{id}", {
        params: { path: { id: args.id } },
        body: args.body,
      });
      if (!response.ok || !data) throw apiError(`user/backups/update/${args.id}`, response.status, error);
      return data as Backup;
    },
  });
}

export function useDeleteUserBackup() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { error, response } = await client.DELETE("/user/backups/{id}", {
        params: { path: { id } },
      });
      if (!response.ok) throw apiError(`user/backups/delete/${id}`, response.status, error);
      return null;
    },
  });
}

export function useRunUserBackup() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { data, error, response } = await client.POST("/user/backups/{id}/run", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/backups/run/${id}`, response.status, error);
      return data as { id: string; status: string };
    },
  });
}

// v1.5.0c — restore a snapshot back to a chosen destination.
// Synchronous: the mutation resolves once the copy is finished, so
// the wizard can render the summary inline. The body matches
// RestoreRequest from the OpenAPI spec.
export type RestoreRequest = components["schemas"]["RestoreRequest"];
export type RestoreResult = components["schemas"]["RestoreResult"];

export function useRestoreUserBackup() {
  return useMutation({
    mutationFn: async (args: { id: string; body: RestoreRequest }) => {
      const { data, error, response } = await client.POST("/user/backups/{id}/restore", {
        params: { path: { id: args.id } },
        body: args.body,
      });
      if (!response.ok || !data) throw apiError(`user/backups/restore/${args.id}`, response.status, error);
      return data as RestoreResult;
    },
  });
}

// useUserBackupSnapshots powers the detail page's snapshot table.
// Returns up to 10 most recent snapshots; server short-circuits to
// [] for mirror-mode backups. We poll alongside the backup record
// itself so a freshly-completed run lands in the table without a
// manual refresh.
export function useUserBackupSnapshots(id: string | null, enabled: boolean) {
  return useQuery<BackupSnapshotEntry[]>({
    queryKey: ["user", "backups", id, "snapshots"],
    queryFn: async () => {
      if (!id) throw new Error("Backup ID required");
      const { data, error, response } = await client.GET("/user/backups/{id}/snapshots", {
        params: { path: { id } },
      });
      if (!response.ok || !data) throw apiError(`user/backups/${id}/snapshots`, response.status, error);
      return data as BackupSnapshotEntry[];
    },
    enabled: !!id && enabled,
    staleTime: 5_000,
    refetchInterval: 10_000,
    retry: 1,
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
  // ADR-0002 (v1.1.0f): roles whose gates retired but whose row stays
  // in the matrix for back-compat. The UI renders a deprecation badge,
  // banner, and an editor alert; new assignments to deprecated roles
  // are gated off in /admin/policies. Field is optional on the wire
  // because Go marshals `omitempty` — older snapshots without the
  // flag still parse.
  deprecated?: boolean;
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
    mutationFn: async (role: Omit<PolicyRole, "seed" | "deprecated"> & { seed?: boolean; deprecated?: boolean }) => {
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

// v1.3.0e CLUSTER.ADMINS — per-cluster admin assignments view.
//
// Convenience read for /admin/clusters/{cid}: returns the assignments
// scoped to this cluster, plus wildcard inheritance (cluster:* and *)
// marked with `inherited=true`. The display name is joined server-side
// so the FE doesn't need a second round-trip per row.
//
// Same `policy:view_matrix` gate as the global /admin/policies GET.
// Mutations still go through useAssignRole / useUnassignRole above.
export type ClusterAdminAssignment = {
  userId: string;
  displayName?: string;
  roleId: string;
  scope: string;
  source?: string;
  inherited: boolean;
};
export type ClusterAdminsResponse = {
  assignments: ClusterAdminAssignment[];
};

export function useClusterAdmins(cid: string) {
  return useQuery<ClusterAdminsResponse>({
    queryKey: ["admin", "clusters", cid, "admins"],
    queryFn: async () => {
      const res = await fetch(
        `/api/v1/admin/clusters/${encodeURIComponent(cid)}/admins`,
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`admin/clusters/${cid}/admins`, res.status, body);
      return body as ClusterAdminsResponse;
    },
    staleTime: 30 * 1000,
    enabled: !!cid,
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

// v1.3.0a — OIDC group-claim -> role auto-mapping hooks
// (/admin/oidc-group-mappings).
//
// Same bare-fetch pattern as the v0.9.0g policy hooks above because
// the endpoints aren't in the OpenAPI spec yet. PUT replaces the full
// list atomically; the GET response is invalidated on every successful
// mutation so the system page re-renders with the new state.
export type OIDCGroupMapping = {
  claim: string;
  claimValue: string;
  roleId: string;
  scope: string;
};
export type OIDCGroupMappingsResponse = {
  mappings: OIDCGroupMapping[];
  updatedAt?: string;
};

export function useOIDCGroupMappings() {
  return useQuery<OIDCGroupMappingsResponse>({
    queryKey: ["admin", "oidc-group-mappings"],
    queryFn: async () => {
      const res = await fetch("/api/v1/admin/oidc-group-mappings", { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/oidc-group-mappings", res.status, body);
      return body as OIDCGroupMappingsResponse;
    },
    staleTime: 30 * 1000,
  });
}

export function useUpdateOIDCGroupMappings() {
  return useMutation({
    mutationFn: async (mappings: OIDCGroupMapping[]) => {
      const res = await fetch("/api/v1/admin/oidc-group-mappings", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mappings }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/oidc-group-mappings/put", res.status, body);
      return body as OIDCGroupMappingsResponse;
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

// v1.4.0c: rangeDays lets the FE switch between 7 / 30 / 90 day
// windows. Backend already accepts from + to as RFC3339 (capped at 90
// days server-side); the hook just computes the window from `now`.
// When rangeDays is omitted the backend default (7d) is used.
export function useUsageSeries(
  cid: string,
  bid: string,
  enabledOrOpts: boolean | { enabled?: boolean; rangeDays?: number } = true,
) {
  const opts =
    typeof enabledOrOpts === "boolean"
      ? { enabled: enabledOrOpts }
      : enabledOrOpts;
  const enabled = opts.enabled ?? true;
  const rangeDays = opts.rangeDays;
  return useQuery<UsageSeriesResponse>({
    queryKey: ["admin", "usage", "series", cid, bid, rangeDays ?? "default"],
    queryFn: async () => {
      const params = new URLSearchParams({ cid, bid });
      if (rangeDays && rangeDays > 0) {
        const to = new Date();
        const from = new Date(to.getTime() - rangeDays * 24 * 60 * 60 * 1000);
        params.set("from", from.toISOString());
        params.set("to", to.toISOString());
      }
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
  // v1.4.0a: pagination offset. Rows to skip from the newest-first
  // ordering before the page is sliced. Combined with limit on the FE
  // to render Prev / Next.
  offset?: number;
}

export interface AuditResponse {
  events: AuditEvent[];
  total: number;
  // v1.4.0a: offset + limit echo back from the server so the FE
  // doesn't have to second-guess the canonical page bounds (server
  // caps + defaults rule).
  offset: number;
  limit: number;
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

// v1.3.0d — persistent invite-token surface for /admin/users. Tokens
// are minted server-side, returned plaintext exactly once on create +
// rotate (operator copies + sends), and persist as bcrypt hashes for
// the public redemption endpoint to check against.
export interface InvitePublic {
  id: string;
  label?: string;
  tokenLast4: string;
  createdBy?: string;
  createdAt: string;
  expiresAt: string;
  expired: boolean;
}

export interface InviteCreateResponse {
  invite: InvitePublic;
  token: string;
}

export function useInvites() {
  return useQuery<InvitePublic[]>({
    queryKey: ["admin", "invites"],
    queryFn: async () => {
      const res = await fetch("/api/v1/admin/invites", { credentials: "include" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/invites", res.status, body);
      return body as InvitePublic[];
    },
    staleTime: 5 * 1000,
  });
}

export function useCreateInvite() {
  return useMutation<InviteCreateResponse, Error, { label?: string; ttlHours?: number }>({
    mutationFn: async (input) => {
      const res = await fetch("/api/v1/admin/invites", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("admin/invites/create", res.status, body);
      return body as InviteCreateResponse;
    },
  });
}

export function useRevokeInvite() {
  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      const res = await fetch(`/api/v1/admin/invites/${encodeURIComponent(id)}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`admin/invites/revoke/${id}`, res.status, body);
      }
    },
  });
}

export function useRotateInvite() {
  return useMutation<InviteCreateResponse, Error, { id: string; ttlHours?: number }>({
    mutationFn: async ({ id, ttlHours }) => {
      const res = await fetch(`/api/v1/admin/invites/${encodeURIComponent(id)}/rotate`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ttlHours }),
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`admin/invites/rotate/${id}`, res.status, body);
      return body as InviteCreateResponse;
    },
  });
}

// v1.4.0c SCRUB.MAINT — Garage block-scrub controls. The GET payload
// folds the capability flag into the response so the UI can render
// the "not supported" branch from the same fetch that drives the
// state read on supported drivers.

export interface ScrubCapability {
  supported: boolean;
  reason?: string;
}

export interface ScrubState {
  running: boolean;
  lastCompleted?: string;
  blocksScanned?: number;
  blocksCorrupt?: number;
  progressPercent?: number;
  message?: string;
}

export interface ScrubResponse {
  capabilities: ScrubCapability;
  state: ScrubState;
}

// useClusterScrub polls every 5 seconds while a scrub is running.
// The interval drops to 30 seconds otherwise so an idle page doesn't
// hammer the backend.
export function useClusterScrub(cid: string) {
  return useQuery<ScrubResponse>({
    queryKey: ["admin", "clusters", cid, "scrub"],
    queryFn: async () => {
      const res = await fetch(
        `/api/v1/admin/clusters/${encodeURIComponent(cid)}/scrub`,
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`admin/clusters/${cid}/scrub`, res.status, body);
      return body as ScrubResponse;
    },
    enabled: !!cid,
    refetchInterval: (q) => {
      const d = q.state.data as ScrubResponse | undefined;
      return d?.state.running ? 5_000 : 30_000;
    },
    staleTime: 0,
    retry: 1,
  });
}

export function useStartClusterScrub(cid: string) {
  return useMutation<ScrubResponse, Error, void>({
    mutationFn: async () => {
      const res = await fetch(
        `/api/v1/admin/clusters/${encodeURIComponent(cid)}/scrub`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
        },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`admin/clusters/${cid}/scrub`, res.status, body);
      return body as ScrubResponse;
    },
  });
}

// v1.6.0d FEDERATION.FE — federated bucket hooks.
//
// The federation endpoints don't ride the OpenAPI spec yet (the v1.6.0c
// API landed too late in the cycle to regenerate); we go around
// openapi-fetch with native fetch the same way the user-region and
// policy hooks do. Hand-typed shapes mirror
// `internal/federation/types.go` + `internal/api/user_federated_buckets.go`
// verbatim so the wire is round-trip-safe.
//
// Polling cadences:
//   - useFederations (list): 10s — matches the engine's default tick
//     so a freshly-completed replicate lands in the list within one
//     tick of the FE refresh.
//   - useFederation (detail): 5s — tighter so the per-replica health
//     pills react quickly when an operator clicks "Resync now" or
//     "Promote to primary".

export type FederationSyncMode = "continuous" | "scheduled";
export type FederationHealth = "" | "in-sync" | "lagging" | "stale" | "broken";

export interface FederationReplicaTarget {
  regionId: string;
  bucket: string;
  lastSync?: string;
  health?: FederationHealth;
  lagBytes?: number;
  lagObjects?: number;
}

export interface FederationPolicy {
  syncMode: FederationSyncMode;
  schedule?: string;
  lagAlertSec: number;
  writeQuorum: number;
  autoFailover?: boolean;
  autoFailoverSec?: number;
}

export interface FederatedBucket {
  id: string;
  ownerUserId: string;
  name: string;
  primary: FederationReplicaTarget;
  replicas: FederationReplicaTarget[];
  policy: FederationPolicy;
  createdAt: string;
  updatedAt: string;
  // computedHealth is the v1.6.0c server-side roll-up — the worst
  // per-replica health across the federation. Used by the list view
  // to render a one-row summary without iterating the replicas array.
  computedHealth?: FederationHealth;
}

export interface FederatedBucketCreateRequest {
  name: string;
  primary: { regionId: string; bucket: string };
  replicas: { regionId: string; bucket: string }[];
  policy?: FederationPolicy;
}

export interface FederationFailoverRequest {
  newPrimaryRegionId: string;
  newPrimaryBucket: string;
}

export function useFederations() {
  return useQuery<FederatedBucket[]>({
    queryKey: ["user", "federations"],
    queryFn: async () => {
      const res = await fetch("/api/v1/user/federated-buckets", {
        credentials: "include",
      });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError("user/federated-buckets", res.status, body);
      return (body as FederatedBucket[]) ?? [];
    },
    staleTime: 5_000,
    refetchInterval: 10_000,
    retry: 1,
  });
}

export function useFederation(id: string | null) {
  return useQuery<FederatedBucket>({
    queryKey: ["user", "federations", id],
    queryFn: async () => {
      if (!id) throw new Error("Federation ID required");
      const res = await fetch(
        `/api/v1/user/federated-buckets/${encodeURIComponent(id)}`,
        { credentials: "include" },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/federated-buckets/${id}`, res.status, body);
      return body as FederatedBucket;
    },
    enabled: !!id,
    staleTime: 2_000,
    refetchInterval: 5_000,
    retry: 1,
  });
}

export function useCreateFederation() {
  return useMutation<FederatedBucket, Error, FederatedBucketCreateRequest>({
    mutationFn: async (body) => {
      const res = await fetch("/api/v1/user/federated-buckets", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const resp = await res.json().catch(() => null);
      if (!res.ok) throw apiError("user/federated-buckets/create", res.status, resp);
      return resp as FederatedBucket;
    },
  });
}

export function useUpdateFederation() {
  return useMutation<FederatedBucket, Error, { id: string; body: FederatedBucketCreateRequest }>({
    mutationFn: async ({ id, body }) => {
      const res = await fetch(
        `/api/v1/user/federated-buckets/${encodeURIComponent(id)}`,
        {
          method: "PUT",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        },
      );
      const resp = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/federated-buckets/update/${id}`, res.status, resp);
      return resp as FederatedBucket;
    },
  });
}

export function useDeleteFederation() {
  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      const res = await fetch(
        `/api/v1/user/federated-buckets/${encodeURIComponent(id)}`,
        { method: "DELETE", credentials: "include" },
      );
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError(`user/federated-buckets/delete/${id}`, res.status, body);
      }
    },
  });
}

export function useFailoverFederation() {
  return useMutation<FederatedBucket, Error, { id: string; body: FederationFailoverRequest }>({
    mutationFn: async ({ id, body }) => {
      const res = await fetch(
        `/api/v1/user/federated-buckets/${encodeURIComponent(id)}/failover`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        },
      );
      const resp = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/federated-buckets/failover/${id}`, res.status, resp);
      return resp as FederatedBucket;
    },
  });
}

// useFederationByTarget hits the v1.6.0e reverse-lookup endpoint:
// "is the (regionId, bucket) currently part of a federation I own?"
// The bucket browser (/files/$regionId/b/$bid) calls this speculatively
// on every render — the endpoint returns 204 No Content when no
// federation matches, surfaced here as `data === null`. A hit returns
// the full FederatedBucket record (with computedHealth + per-replica
// health) so the badge can render counts inline.
//
// staleTime is 30s because federation membership doesn't change often
// (operators don't add/remove replicas on the second), but we want a
// reasonably fresh count when they hop between buckets in the same
// session. retry: false so a transient 5xx doesn't bury the bucket
// browser under retries — the badge is best-effort.
export function useFederationByTarget(
  regionId: string,
  bucket: string | undefined,
) {
  return useQuery<FederatedBucket | null>({
    queryKey: ["federation-by-target", regionId, bucket],
    queryFn: async () => {
      if (!bucket) return null;
      const url = `/api/v1/user/federated-buckets/by-target?regionId=${encodeURIComponent(regionId)}&bucket=${encodeURIComponent(bucket)}`;
      const res = await fetch(url, { credentials: "include" });
      if (res.status === 204) return null;
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError("federation-by-target", res.status, body);
      }
      return (await res.json()) as FederatedBucket;
    },
    enabled: !!bucket,
    staleTime: 30_000,
    retry: false,
  });
}

export function useResyncFederation() {
  return useMutation<{ id: string; status: string }, Error, string>({
    mutationFn: async (id) => {
      const res = await fetch(
        `/api/v1/user/federated-buckets/${encodeURIComponent(id)}/resync`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
        },
      );
      const body = await res.json().catch(() => null);
      if (!res.ok) throw apiError(`user/federated-buckets/resync/${id}`, res.status, body);
      return body as { id: string; status: string };
    },
  });
}
