import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      // В dev режиме API бэкенда живёт на :8080
      "/api": "http://localhost:8080",
    },
  },
});
