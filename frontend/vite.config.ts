import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import path from "node:path";

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
      autoCodeSplitting: true,
      // Non-route files in routes/ use the "-" prefix (tsr.config.json
      // routeFileIgnorePrefix). Example: -AdminLanding.tsx beside
      // admin/index.tsx.
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
    assetsDir: "assets",
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
