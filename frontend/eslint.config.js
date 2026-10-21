import pluginVue from "eslint-plugin-vue";
import { defineConfigWithVueTs, vueTsConfigs } from "@vue/eslint-config-typescript";
import prettier from "@vue/eslint-config-prettier/skip-formatting";

// The tree's import rules, by page: a page folder imports from stores/, queries.ts, lib/, ui/ and itself (its own
// subfolders included: pages/ide/editor, pages/ide/draw), never another page or the frame's components; ui/ and
// lib/ sit under every page and import none; the frame and the stores reach a page only through the router, the
// stores reading the IDE's editor modules alone. The patterns are relative and climb to the pages folder, so a page's
// own subfolders never match.
const imports = (files, patterns) => ({ files, rules: { "no-restricted-imports": ["error", { patterns }] } });
const frame = (up) => ({ group: [up + "../app/*.vue"], message: "a page never imports the frame's components" });
const sibling = (up) => ({
  group: [up + "[a-z]*/*", up + "[a-z]*/**"],
  message: "a page never imports another page's folder",
});

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
  imports(["src/pages/*/*.{ts,vue}"], [frame("../"), sibling("../")]),
  imports(["src/pages/*/*/*.{ts,vue}"], [frame("../../"), sibling("../../")]),
  imports(
    ["src/ui/**/*.{ts,vue}", "src/lib/**"],
    [{ group: ["../pages/**", "../app/**", "!../app/pages"], message: "ui and lib import no page or frame module" }],
  ),
  imports(
    ["src/stores/**", "src/app/**"],
    [
      {
        group: ["../pages/**", "!../pages/ide/editor/**"],
        message: "the frame and the stores reach a page only through the router",
      },
    ],
  ),
  prettier,
);
