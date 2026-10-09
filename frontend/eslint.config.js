import pluginVue from "eslint-plugin-vue";
import { defineConfigWithVueTs, vueTsConfigs } from "@vue/eslint-config-typescript";
import prettier from "@vue/eslint-config-prettier/skip-formatting";

export default defineConfigWithVueTs(
  { ignores: ["bindings/**", "dist/**", "auto-imports.d.ts", "components.d.ts"] },
  pluginVue.configs["flat/recommended"],
  vueTsConfigs.recommendedTypeChecked,
  {
    rules: {
      "vue/no-mutating-props": "error",
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-non-null-assertion": "error",
    },
  },
  prettier,
);
