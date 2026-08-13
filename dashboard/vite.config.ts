import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    // Proxy keeps the browser on one origin, so the dashboard works even if
    // CORS is tightened on the Go side later.
    proxy: {
      "/api": "http://localhost:8080",
      "/jobs": "http://localhost:8080",
    },
  },
});
