/**
 * The routes: the Library, the wizard, and a workspace's pages under /workspace/:id, each given the id as a
 * prop. The guard alone sets the active workspace.
 */
import { createRouter, createWebHashHistory } from "vue-router";
import { Events } from "@wailsio/runtime";
import UnsavedChangesModal from "../components/UnsavedChangesModal.vue";
import { useIdeStore } from "../stores/ide";
import { useWorkspaceStore } from "../stores/workspace";
import { dockEditors, isMain, others } from "../windows";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: { name: "library" } },
    { path: "/library", name: "library", component: () => import("../pages/LibraryPage.vue") },
    { path: "/wizard", name: "wizard", component: () => import("../pages/WizardPage.vue") },
    {
      path: "/workspace/:id",
      children: [
        { path: "", name: "workspace-ide", component: () => import("../pages/WorkspaceIdePage.vue"), props: true },
        { path: "graph", name: "event-graph", component: () => import("../pages/EventGraphPage.vue"), props: true },
        {
          path: "health",
          name: "health",
          component: () => import("../pages/HealthPage.vue"),
          // a link opens a card, and a row by its name
          props: (to) => ({ ...to.params, card: to.query.card, name: to.query.name }),
        },
        { path: "patch-notes", name: "patch-notes", component: () => import("../pages/PatchNotesPage.vue"), props: true },
        {
          path: "release",
          name: "release",
          component: () => import("../pages/ReleasePage.vue"),
          // a link opens a step: the listing, the version and change note, or review and upload
          props: (to) => ({ ...to.params, step: to.query.step }),
        },
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

const unsavedModal = useOverlay().create(UnsavedChangesModal);

router.beforeEach(async (to, from) => {
  // a window of one thing sends a link to another page to the main window, and the main window one to a page in a
  // window of its own there
  const page = `page:${String(to.name)}`;
  const elsewhere = isMain ? (others.has(page) ? page : "") : from.name && to.name !== from.name ? "main" : "";
  if (elsewhere) {
    void Events.Emit("window:show", { to: elsewhere, route: to.fullPath, path: "", line: -1, col: 0 });
    return false;
  }
  if (!("id" in to.params)) return;
  // another workspace's files would take the editors' place: the editor windows hand theirs back, then the unsaved
  // edits are saved or dropped, or we stay
  const ide = useIdeStore();
  if (isMain && ide.editing && to.params.id !== ide.editing) await dockEditors();
  if (ide.unsaved.size && ide.editing && to.params.id !== ide.editing && !(await unsavedModal.open())) return false;
  useWorkspaceStore().select(to.params.id);
});

export default router;
