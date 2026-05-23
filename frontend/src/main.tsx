import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import "./index.css";
import { routeTree } from "./routeTree.gen";
import { applyCookieThemeEarly } from "./shared/theme/useTheme";
import { AuthModeProvider } from "./shared/auth/mode";
import { AuthModeHydrator } from "./shared/auth/AuthModeHydrator";
import { ElevationProvider } from "./shared/auth/elevation";
import { ClusterUnlockProvider } from "./shared/auth/clusterUnlock";

// Apply the cookie-stored theme before React paints so users with a
// non-system preference don't see a flash of the wrong palette.
applyCookieThemeEarly();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      {/* ADR-0003 v1.2.0b: sudo-style mode state lives at the root */}
      {/* so the persona pill, elevation modal, and any per-route */}
      {/* guard all read the same source. Hydrator pulls the */}
      {/* initial mode off /auth/me; ElevationProvider exposes the */}
      {/* modal + runWithElevation wrapper for destructive ops. */}
      <AuthModeProvider>
        <AuthModeHydrator />
        <ElevationProvider>
          {/* v1.12.0a (ADR-0007): cluster-unlock modal at root so a */}
          {/* 423 LOCKED from any mutation opens the prompt and a */}
          {/* successful unlock retries the original call. */}
          <ClusterUnlockProvider>
            <RouterProvider router={router} />
          </ClusterUnlockProvider>
        </ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
