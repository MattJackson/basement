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

export function useTestClusterQuery(cid: string) {
  // Manual-only — never auto-fires on mount, never polls. Caller
  // triggers via .refetch(). HealthCheck on Garage takes 10–20s
  // because /v1/health round-trips the whole cluster; auto-polling
  // it per-cluster-row at 30s cadence saturates the server.
  return useQuery<components["schemas"]["ConnectionTestResult"], Error>({
    queryKey: ["admin", "clusters", cid, "_test"],
    queryFn: async () => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/_test", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`testCluster/${cid}`, response.status, error);
      return data as components["schemas"]["ConnectionTestResult"];
    },
    enabled: false,
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
      const params: Record<string, string> = {};
      if (prefix) params.prefix = prefix;
      if (token) params.token = token;

      let url = `/api/v1/user/clusters/${cid}/buckets/${bid}/objects`;
      const qs = new URLSearchParams(params).toString();
      if (qs) url += `?${qs}`;

      const { data, error, response } = await client.GET(url as any, { params: { query: params } });
      if (!response.ok || !data) throw apiError(`user/objects/${cid}/${bid}`, response.status, error);
      return data as components["schemas"]["ObjectPage"];
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
