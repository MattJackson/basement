import { useMutation, useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/types.gen";
import { useNavigate } from "@tanstack/react-router";

type Bucket = components["schemas"]["Bucket"];

// v1.13.18: User response with active role selector fields
export interface UserResponse {
  id?: string;
  username: string;
  role: "admin" | "user";
  uiAdmin: boolean;
  mode?: "user" | "admin" | "elevated";
  modeExpiresAt?: number;
  activeRole?: {
    kind: "user" | "cluster-admin" | "ui-admin";
    cluster?: string;
  };
  availableRoles?: Array<{
    kind: "user" | "cluster-admin" | "ui-admin";
    label: string;
    cluster?: string;
  }>;
  oidcUser?: boolean;
}

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
      navigate({ to: "/" });
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
      // Per-cluster key list (count + access-bucket badges per row).
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "keys"] });
      // Bucket-side mirror — every affected bucket's "Attached keys"
      // section needs to refetch. Cheapest correct move is to invalidate
      // the whole cluster's bucket list and aggregate list; both pages
      // re-pull on mount.
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "buckets"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
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
    onSuccess: (_data, variables) => {
      // v1.11.0.15: keys are per-cluster; the orphan global
      // /admin/keys route was removed. Land back on the cluster
      // detail page (which renders the keys section the operator
      // came from) and invalidate the per-cluster keys cache so
      // the deleted row is gone on arrival. There's no standalone
      // /admin/clusters/{cid}/keys/ index route — the cluster
      // detail IS the canonical "all keys for this cluster" view.
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", variables.cid, "keys"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "keys"] });
      navigate({ to: "/admin/clusters/$cid", params: { cid: variables.cid } });
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

// Layout editor mutations — per-cluster stage/apply/revert.

export function useStageLayoutChange(cid: string) {
  const queryClient = useQueryClient();
  return useMutation<components["schemas"]["LayoutDiff"], Error, components["schemas"]["LayoutChange"]>({
    mutationFn: async (change) => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/layout/stage", {
        params: { path: { cid } },
        body: change,
      });
      if (!response.ok || !data) throw apiError(`stageLayout/${cid}`, response.status, error);
      return data as components["schemas"]["LayoutDiff"];
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "layout"] });
    },
  });
}

export function useApplyLayout(cid: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: async () => {
      const { response, error } = await client.POST("/admin/clusters/{cid}/layout/apply", {
        params: { path: { cid } },
      });
      if (!response.ok) throw apiError(`applyLayout/${cid}`, response.status, error);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "layout"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "nodes"] });
    },
  });
}

export function useRevertLayout(cid: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data, error, response } = await client.POST("/admin/clusters/{cid}/layout/revert", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) throw apiError(`revertLayout/${cid}`, response.status, error);
      return data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "clusters", cid, "layout"] });
    },
  });
}

// v1.13.18: Active role switching mutation
export interface SwitchActiveRoleRequest {
  kind: "user" | "cluster-admin" | "ui-admin";
  cluster?: string; // only required for cluster-admin
}

export function useSwitchActiveRole() {
  const queryClient = useQueryClient();
  return useMutation<UserResponse, Error, SwitchActiveRoleRequest>({
    mutationFn: async (req) => {
      const res = await fetch("/api/v1/auth/active-role", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw apiError("active-role/switch", res.status, body);
      }
      return (await res.json()) as UserResponse;
    },
    onSuccess: (user) => {
      queryClient.setQueryData(["auth", "me"], user);
      queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
    },
  });
}



