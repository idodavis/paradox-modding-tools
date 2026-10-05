/**
 * The routes: the Library, the wizard, and a workspace's pages under /workspace/:id, each given the id as a
 * prop. The guard alone sets the active workspace.
 */
import { createRouter, createWebHashHistory } from "vue-router";
import { useWorkspaceStore } from "../stores/workspace";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: { name: "library" } },
    { path: "/library", name: "library", component: () => import("../pages/LibraryPage.vue") },
    { path: "/wizard", name: "wizard", component: () => import("../pages/WizardPage.vue") },
    {
      path: "/workspace/:id",
      children: [
        // the IDE's strip sits in the workbench, which App keeps mounted behind every page
        {
          path: "",
          name: "workspace-ide",
          components: { ide: () => import("../pages/WorkspaceIdePage.vue") },
          props: { ide: true },
        },
        { path: "graph", name: "event-graph", component: () => import("../pages/EventGraphPage.vue"), props: true },
        {
          path: "health",
          name: "health",
          component: () => import("../pages/HealthPage.vue"),
          // a link opens a card, and a row by its name
          props: (to) => ({ ...to.params, card: to.query.card, name: to.query.name }),
        },
        { path: "patch-notes", name: "patch-notes", component: () => import("../pages/PatchNotesPage.vue"), props: true },
        { path: "release", name: "release", component: () => import("../pages/ReleasePage.vue"), props: true },
        {
          path: "settings",
          name: "workspace-settings",
          component: () => import("../pages/WorkspaceSettingsPage.vue"),
          // the section is in the query, so the palette and links open the one they name
          props: (to) => ({ ...to.params, section: to.query.section }),
        },
      ],
    },
  ],
});

router.beforeEach((to) => {
  if ("id" in to.params) useWorkspaceStore().select(to.params.id);
});

export default router;
