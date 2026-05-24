/// <reference types="vitest" />
import { defineConfig } from "vite";
import path from "node:path";

export default defineConfig({
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/tests/setup.ts"],
    injectMockGlobals: [
      "__BUILD_COMMIT__ = 'test-build-123'",
    ],
    coverage: {
      include: ["src/**/*.{ts,tsx}"],
      reporter: ["text", "html"],
    },
  },
});
