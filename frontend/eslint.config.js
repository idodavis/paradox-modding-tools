import pluginVue from "eslint-plugin-vue";
import { defineConfigWithVueTs, vueTsConfigs } from "@vue/eslint-config-typescript";
import prettier from "@vue/eslint-config-prettier/skip-formatting";

// The target shape's import rules: a page folder imports from stores/, queries.ts, lib/, composables/, ui/ and
// itself, never another page or the frame's components; editor/ and draw/ are the IDE page's (the stores import the
// editor too), and draw/ itself imports no page or frame module; ui/, composables/ and lib/ sit under every page; the
// frame and the stores reach a page only through the router. The patterns are relative so a package's own "pages",
// "editor" or "draw" folder never matches.
const imports = (files, patterns) => ({ files, rules: { "no-restricted-imports": ["error", { patterns }] } });
const frame = { group: ["../../app/*.vue"], message: "a page never imports the frame's components" };
const sibling = { group: ["../[a-z]*/*", "../[a-z]*/**"], message: "a page never imports another page's folder" };

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
  // the pane's floor is a Viewport, one word that is no HTML element's
  { files: ["src/draw/Viewport.vue"], rules: { "vue/multi-word-component-names": "off" } },
  imports(
    ["src/pages/**/*.{ts,vue}"],
    [
      frame,
      { group: ["../../editor/**"], message: "only pages/ide and the stores import the editor" },
      { group: ["../../draw/**"], message: "only pages/ide imports draw" },
      sibling,
    ],
  ),
  imports(["src/pages/ide/**/*.{ts,vue}"], [frame, sibling]),
  imports(
    ["src/draw/**/*.{ts,vue}"],
    [{ group: ["../pages/**", "../app/**", "!../app/pages"], message: "draw imports no page or frame module" }],
  ),
  imports(
    ["src/ui/**/*.{ts,vue}", "src/composables/**", "src/lib/**"],
    [
      {
        group: ["../pages/**", "../app/**", "!../app/pages", "../editor/**"],
        message: "ui, composables and lib import no page, frame or editor module",
      },
    ],
  ),
  imports(
    ["src/stores/**", "src/app/**"],
    [{ group: ["../pages/**"], message: "the frame and the stores reach a page only through the router" }],
  ),
  prettier,
);
