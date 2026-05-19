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

  return useMutation<{ bucket: Bucket }, Error, { global_alias: string }>({
    mutationFn: async (spec) => {
      const { data, error, response } = await client.POST("/admin/buckets", {
        body: spec,
      });
      if (!response.ok || !data) throw apiError("createBucket", response.status, error);
      return data as any;
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      navigate({ to: "/admin/buckets/$id", params: { id: variables.global_alias } });
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
        body: update as any,
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

export function useDeleteBucket() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      const { data: _data, error, response } = await client.DELETE("/admin/buckets/{id}", {
        params: { path: { id } },
      });
      if (!response.ok) throw apiError(`deleteBucket/${id}`, response.status, error);
    },
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets", id] });
      navigate({ to: "/admin" });
    },
  });
}
