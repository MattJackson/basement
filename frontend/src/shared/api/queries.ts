import { useQuery } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/types.gen";

type Node = components["schemas"]["Node"];
type Layout = components["schemas"]["Layout"];
type Caps = components["schemas"]["Caps"];

export function useCapabilities() {
  return useQuery<Caps>({
    queryKey: ["capabilities"],
    queryFn: async () => {
      const { data, response } = await client.GET("/capabilities");
      if (response.status === 401) {
        throw new Error("Unauthorized");
      }
      if (!response.ok || !data) {
        throw new Error(`Failed to fetch capabilities (status ${response.status})`);
      }
      return data as Caps;
    },
  });
}

export function useNodes() {
  return useQuery<Node[]>({
    queryKey: ["admin", "nodes"],
    queryFn: async () => {
      const { data, response } = await client.GET("/admin/nodes");
      if (response.status === 401) {
        throw new Error("Unauthorized");
      }
      if (!response.ok || !data) {
        throw new Error(`Failed to fetch nodes (status ${response.status})`);
      }
      return data as Node[];
    },
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
  });
}

export function useLayout() {
  return useQuery<Layout>({
    queryKey: ["admin", "layout"],
    queryFn: async () => {
      const { data, response } = await client.GET("/admin/layout");
      if (response.status === 401) {
        throw new Error("Unauthorized");
      }
      if (!response.ok || !data) {
        throw new Error(`Failed to fetch layout (status ${response.status})`);
      }
      return data as Layout;
    },
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
  });
}
