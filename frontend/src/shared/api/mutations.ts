import { useMutation, useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/types.gen";
import { useNavigate } from "@tanstack/react-router";

type Bucket = components["schemas"]["Bucket"];

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

// Buckets are now connection-scoped. All bucket mutations thread cid
// through so the registry resolves the right driver and so the
// invalidation keys segregate per cluster.

export function useCreateBucket() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<Bucket, Error, { cid: string; alias: string }>({
    mutationFn: async ({ cid, alias }) => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/buckets", {
        params: { path: { cid } },
        body: { alias },
      });
      if (!response.ok || !data) throw apiError("createBucket", response.status, error);
      return data;
    },
    onSuccess: (bucket, { cid }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "buckets"] });
      navigate({ to: "/admin/clusters/$cid/buckets/$id", params: { cid, id: bucket.id } });
    },
  });
}

export function useUpdateBucket() {
  const queryClient = useQueryClient();

  return useMutation<
    Bucket,
    Error,
    { cid: string; id: string; update: { aliases?: string[] } | { quotas?: { max_size?: number | null; max_objects?: number | null } | undefined } }
  >({
    mutationFn: async ({ cid, id, update }) => {
      const { data: _data, error, response } = await client.PATCH("/admin/clusters/{cid}/buckets/{id}", {
        params: { path: { cid, id } },
        body: update as never,
      });
      if (!response.ok || !_data) throw apiError(`updateBucket/${id}`, response.status, error);
      return _data as Bucket;
    },
    onSuccess: (_, { cid, id }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "buckets", id] });
    },
  });
}

/**
 * Two-phase delete: POST /_arm-delete then DELETE with X-Confirm-Delete.
 * Connection-scoped — the token is bound to (bucket id, user); the path
 * carries the cid.
 */
export function useDeleteBucket() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, { cid: string; id: string }>({
    mutationFn: async ({ cid, id }) => {
      const arm = await client.POST("/admin/clusters/{cid}/buckets/{id}/_arm-delete", {
        params: { path: { cid, id } },
      });
      if (!arm.response.ok || !arm.data?.token) {
        throw apiError(`armDeleteBucket/${id}`, arm.response.status, arm.error);
      }
      const del = await client.DELETE("/admin/clusters/{cid}/buckets/{id}", {
        params: {
          path: { cid, id },
          header: { "X-Confirm-Delete": arm.data.token },
        },
        headers: { "X-Confirm-Delete": arm.data.token },
      });
      if (!del.response.ok) {
        throw apiError(`deleteBucket/${id}`, del.response.status, del.error);
      }
    },
    onSuccess: (_, { cid, id }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "buckets", id] });
      navigate({ to: "/admin" });
    },
  });
}

/**
 * Create a new access key. Returns the key with its secretAccessKey
 * — Garage returns the secret ONLY on this response and never again.
 *
 * The mutation does NOT auto-navigate. The caller is expected to
 * surface the secret to the user (one-shot dialog with copy button)
 * BEFORE navigating to the detail page, otherwise the secret is
 * lost forever and the key is unusable.
 */
export function useCreateKey() {
  const queryClient = useQueryClient();

  return useMutation<components["schemas"]["Key"], Error, { cid: string; name: string }>({
    mutationFn: async ({ cid, name }) => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/keys", {
        params: { path: { cid } },
        body: { name },
      });
      if (!response.ok || !data) throw apiError("createKey", response.status, error);
      return data;
    },
    onSuccess: (_, { cid }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "keys"] });
      // No navigate — caller surfaces secretAccessKey first.
    },
  });
}

/**
 * Update a key's per-bucket permissions. Invalidates [admin, keys] and [admin, keys, id].
 */
export function useUpdateKeyPermissions() {
  const queryClient = useQueryClient();

  return useMutation<components["schemas"]["Key"], Error, { cid: string; id: string; permissions: components["schemas"]["BucketPermission"][] }>({
    mutationFn: async ({ cid, id, permissions }) => {
      const { data, error, response } = await client.PATCH("/admin/clusters/{cid}/keys/{id}", {
        params: { path: { cid, id } },
        body: { bucketsPermissions: permissions },
      });
      if (!response.ok || !data) throw apiError(`updateKeyPermissions/${id}`, response.status, error);
      return data;
    },
    onSuccess: (_, { cid, id }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "keys", id] });
    },
  });
}

export function useDeleteKey() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, { cid: string; id: string }>({
    mutationFn: async ({ cid, id }) => {
      const arm = await client.POST("/admin/clusters/{cid}/keys/{id}/_arm-delete", {
        params: { path: { cid, id } },
      });
      if (!arm.response.ok || !arm.data?.token) {
        throw apiError(`armDeleteKey/${id}`, arm.response.status, arm.error);
      }
      const del = await client.DELETE("/admin/clusters/{cid}/keys/{id}", {
        params: {
          path: { cid, id },
          header: { "X-Confirm-Delete": arm.data.token },
        },
        headers: { "X-Confirm-Delete": arm.data.token },
      });
      if (!del.response.ok) {
        throw apiError(`deleteKey/${id}`, del.response.status, del.error);
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      navigate({ to: "/admin/keys" });
    },
  });
}

export function useCreateCluster() {
  const queryClient = useQueryClient();

  return useMutation<components["schemas"]["Connection"], Error, components["schemas"]["ConnectionSpec"]>({
    mutationFn: async (spec) => {
      const { data, error, response } = await client.POST("/admin/clusters", {
        body: spec,
      });
      if (!response.ok || !data) throw apiError("createCluster", response.status, error);
      return data as components["schemas"]["Connection"];
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters"] });
    },
  });
}

export function useUpdateCluster(cid: string) {
  const queryClient = useQueryClient();

  return useMutation<
    components["schemas"]["Connection"],
    Error,
    components["schemas"]["ConnectionUpdate"]
  >({
    mutationFn: async (update) => {
      const { data, error, response } = await client.PATCH("/admin/clusters/{cid}", {
        params: { path: { cid } },
        body: update as never,
      });
      if (!response.ok || !data) throw apiError(`updateCluster/${cid}`, response.status, error);
      return data as components["schemas"]["Connection"];
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid] });
    },
  });
}

/**
 * Two-phase delete for clusters: POST /_arm-delete to mint a short-
 * lived HMAC token, then DELETE with X-Confirm-Delete header carrying
 * the token. Both phases happen inside one mutation so the UI calls
 * `deleteCluster.mutate(cid)` and gets a single isPending/onError.
 */
export function useDeleteCluster() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, string>({
    mutationFn: async (cid) => {
      const arm = await client.POST("/admin/clusters/{cid}/_arm-delete", {
        params: { path: { cid } },
      });
      if (!arm.response.ok || !arm.data?.token) {
        throw apiError(`armDeleteCluster/${cid}`, arm.response.status, arm.error);
      }
      const del = await client.DELETE("/admin/clusters/{cid}", {
        params: {
          path: { cid },
          header: { "X-Confirm-Delete": arm.data.token },
        },
        headers: { "X-Confirm-Delete": arm.data.token },
      });
      if (!del.response.ok) {
        throw apiError(`deleteCluster/${cid}`, del.response.status, del.error);
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters"] });
      navigate({ to: "/admin/clusters" });
    },
  });
}

export function useTestCluster(cid: string) {
  const queryClient = useQueryClient();

  return useMutation<components["schemas"]["ConnectionTestResult"], Error, void>({
    mutationFn: async () => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/_test", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`testCluster/${cid}`, response.status, error);
      return data as components["schemas"]["ConnectionTestResult"];
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid] });
    },
  });
}
