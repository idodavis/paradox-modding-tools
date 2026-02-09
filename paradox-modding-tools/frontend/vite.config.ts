import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import wails from "@wailsio/runtime/plugins/vite";
import tailwindcss from '@tailwindcss/vite'
import path from "path";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), wails("./bindings"), tailwindcss()],
  resolve: {
    alias: {
      "@services": path.resolve(__dirname, "bindings/paradox-modding-tools/services"),
      "@components": path.resolve(__dirname, "src/lib/components"),
      "@pages": path.resolve(__dirname, "src/lib/pages"),
      "@stores": path.resolve(__dirname, "src/lib/stores"),
      "@assets": path.resolve(__dirname, "src/assets"),
      "@utils": path.resolve(__dirname, "src/lib/utils"),
    },
  },
});
