import { Outlet, createRootRoute } from "@tanstack/react-router";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

export const Route = createRootRoute({
  component: () => (
    <TooltipProvider>
      <>
        <Outlet />
        <Toaster richColors position="bottom-right" />
      </>
    </TooltipProvider>
  ),
});
