import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate, useLocation } from "@tanstack/react-router";
import { useUser } from "./useUser";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

interface ProtectedRouteProps {
  children: ReactNode;
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { data, isLoading } = useUser();

  useEffect(() => {
    if (!isLoading && !data) {
      const currentPath = location.href.split("#")[0];
      navigate({ to: "/admin/login", search: { next: currentPath } });
    }
  }, [isLoading, data, navigate, location]);

  if (isLoading || !data) {
    return <LoadingSpinner />;
  }

  return <>{children}</>;
}
