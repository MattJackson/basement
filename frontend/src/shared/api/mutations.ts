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

export function useCreateBucket() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<Bucket, Error, { alias: string }>({
    mutationFn: async (spec) => {
      const { data, error, response } = await client.POST("/admin/buckets", {
        body: spec,
      });
      if (!response.ok || !data) throw apiError("createBucket", response.status, error);
      return data;
    },
    onSuccess: (bucket) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      navigate({ to: "/admin/buckets/$id", params: { id: bucket.id } });
    },
  });
}

export function useUpdateBucket() {
  const queryClient = useQueryClient();

  return useMutation<
    Bucket,
    Error,
    { id: string; update: { aliases?: string[] } | { quotas?: { max_size?: number | null; max_objects?: number | null } | undefined } }
  >({
    mutationFn: async ({ id, update }) => {
      const { data: _data, error, response } = await client.PATCH("/admin/buckets/{id}", {
        params: { path: { id } },
        body: update as never,
      });
      if (!response.ok || !_data) throw apiError(`updateBucket/${id}`, response.status, error);
      return _data as Bucket;
    },
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets", id] });
    },
  });
}

/**
 * Two-phase delete: POST /_arm-delete to mint a short-lived HMAC
 * token, then DELETE with X-Confirm-Delete header carrying the token.
 * No single curl-by-hand can destroy a bucket.
 */
export function useDeleteBucket() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      // Phase 1 — arm. Server verifies the bucket exists and mints a
      // 60-second HMAC token bound to (bucket id, current user).
      const arm = await client.POST("/admin/buckets/{id}/_arm-delete", {
        params: { path: { id } },
      });
      if (!arm.response.ok || !arm.data?.token) {
        throw apiError(`armDeleteBucket/${id}`, arm.response.status, arm.error);
      }
      // Phase 2 — fire. Token goes in X-Confirm-Delete; server
      // re-verifies it matches the requested bucket + user before
      // calling the backend delete.
      const del = await client.DELETE("/admin/buckets/{id}", {
        params: {
          path: { id },
          header: { "X-Confirm-Delete": arm.data.token },
        },
        headers: { "X-Confirm-Delete": arm.data.token },
      });
      if (!del.response.ok) {
        throw apiError(`deleteBucket/${id}`, del.response.status, del.error);
      }
    },
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets", id] });
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

  return useMutation<components["schemas"]["Key"], Error, { name: string }>({
    mutationFn: async (spec) => {
      const { data, error, response } = await client.POST("/admin/keys", {
        body: spec,
      });
      if (!response.ok || !data) throw apiError("createKey", response.status, error);
      return data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      // No navigate — the caller surfaces secretAccessKey first.
    },
  });
}

/**
 * Update a key's per-bucket permissions. Invalidates [admin, keys] and [admin, keys, id].
 */
export function useUpdateKeyPermissions() {
  const queryClient = useQueryClient();

  return useMutation<components["schemas"]["Key"], Error, { id: string; permissions: components["schemas"]["BucketPermission"][] }>({
    mutationFn: async ({ id, permissions }) => {
      const { data, error, response } = await client.PATCH("/admin/keys/{id}", {
        params: { path: { id } },
        body: { bucketsPermissions: permissions },
      });
      if (!response.ok || !data) throw apiError(`updateKeyPermissions/${id}`, response.status, error);
      return data;
    },
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "keys", id] });
    },
  });
}

/**
 * Two-phase delete for access keys: POST /_arm-delete to mint a short-lived HMAC
 * token, then DELETE with X-Confirm-Delete header carrying the token.
 */
export function useDeleteKey() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      // Phase 1 — arm. Server verifies the key exists and mints a
      // 60-second HMAC token bound to (key id, current user).
      const arm = await client.POST("/admin/keys/{id}/_arm-delete", {
        params: { path: { id } },
      });
      if (!arm.response.ok || !arm.data?.token) {
        throw apiError(`armDeleteKey/${id}`, arm.response.status, arm.error);
      }
      // Phase 2 — fire. Token goes in X-Confirm-Delete; server
      // re-verifies it matches the requested key + user before
      // calling the backend delete.
      const del = await client.DELETE("/admin/keys/{id}", {
        params: {
          path: { id },
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
