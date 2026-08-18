import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": import.meta.dirname } },
  test: { environment: "jsdom", include: ["**/*.test.{ts,tsx}"], exclude: ["node_modules", "e2e", ".next"] },
});
