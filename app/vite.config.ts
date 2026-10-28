import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@layer8/types": fileURLToPath(new URL("../daemon/src/types.ts", import.meta.url)) },
  },
  clearScreen: false,
  server: { port: 1420, strictPort: true },
});
