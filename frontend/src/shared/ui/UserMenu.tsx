import { useState } from "react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useNavigate } from "@tanstack/react-router";
import { client } from "@/shared/api/client";

export function UserMenu() {
  const navigate = useNavigate();
  const [username] = useState("admin");
  const [role] = useState<"admin" | "user">("admin");

  const handleLogout = async () => {
    try {
      await client.POST("/auth/logout");
    } catch (error) {
      console.error("Logout failed:", error);
    } finally {
      await navigate({ to: "/admin/login" });
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm font-medium hover:bg-muted/50 transition-colors">
        <span className="h-8 w-8 rounded-full bg-primary flex items-center justify-center text-primary-foreground">
          {username.charAt(0).toUpperCase()}
        </span>
        <span className="hidden sm:inline">{username}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuLabel>
          <div className="flex flex-col">
            <span className="font-medium">{username}</span>
            <span className="text-xs text-muted-foreground capitalize">{role}</span>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleLogout}>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
