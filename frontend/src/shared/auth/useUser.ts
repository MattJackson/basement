import { useQuery } from "@tanstack/react-query";
import { client } from "@/shared/api/client";

type UserResponse = {
  id: string;
  username: string;
  role: "admin" | "user";
};

export function useUser() {
  const result = useQuery<UserResponse | undefined>({
    queryKey: ["auth", "me"],
    queryFn: async () => {
      const { data, response } = await client.GET("/auth/me");

      if (response.status === 401) {
        return undefined;
      }
      if (!response.ok || !data) {
        throw new Error(`Failed to fetch user (status ${response.status})`);
      }
      return data as UserResponse;
    },
    staleTime: 5 * 60 * 1000,
    retry: (failureCount, error) => {
      if (error instanceof Error && error.message.includes("status 401")) return false;
      return failureCount < 2;
    },
  });

  return {
    data: result.data,
    isLoading: result.isLoading,
    isError: result.isError,
  };
}
