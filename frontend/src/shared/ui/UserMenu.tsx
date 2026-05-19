import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { useUser } from "@/shared/auth/useUser";

export function UserMenu() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: user } = useUser();

  const username = user?.username ?? "—";
  const role = user?.role ?? "—";
  const initial = (user?.username ?? "?").charAt(0).toUpperCase();

  const handleLogout = async () => {
    try {
      await client.POST("/auth/logout");
    } catch (error) {
      console.error("logout request failed:", error);
    }
    // Clear cached user state so ProtectedRoute sees "logged out" and
    // redirects on the next render.
    queryClient.removeQueries({ queryKey: ["auth", "me"] });
    await navigate({ to: "/admin/login" });
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm font-medium hover:bg-muted/50 transition-colors">
        <span className="h-8 w-8 rounded-full bg-primary flex items-center justify-center text-primary-foreground">
          {initial}
        </span>
        <span className="hidden sm:inline">{username}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuGroup>
          <DropdownMenuLabel>
            <div className="flex flex-col">
              <span className="font-medium">{username}</span>
              <span className="text-xs text-muted-foreground capitalize">{role}</span>
            </div>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleLogout}>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
