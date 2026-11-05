// The tests run in Node: the stores and the editor's pure modules, with the Wails runtime and the bound services
// mocked. The app's vite.config.ts is not merged in: its plugins (Vue, Nuxt UI, Wails, Tailwind) draw pages.
import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: { include: ["src/**/*.test.ts"], environment: "node", setupFiles: ["vitest.setup.ts"] },
  resolve: {
    alias: {
      "@services": path.resolve(import.meta.dirname, "bindings/paradox-modding-tools/services"),
      "@internal": path.resolve(import.meta.dirname, "bindings/paradox-modding-tools/internal"),
    },
  },
});
