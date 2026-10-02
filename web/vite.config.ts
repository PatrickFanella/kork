import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const backend = "http://localhost:8080";
const proxied = ["/api", "/preview", "/u", "/static", "/go"];

export default defineConfig({
  base: "/app/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: Object.fromEntries(proxied.map((p) => [p, { target: backend, changeOrigin: false }])),
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
