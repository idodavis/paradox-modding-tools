/**
 * The routes: the Library, the wizard, and a workspace's pages under /workspace/:id, each given the id as a
 * prop. The guard alone sets the active workspace.
 */
import { createRouter, createWebHashHistory } from "vue-router";
import { Events } from "@wailsio/runtime";
import UnsavedChangesModal from "../ui/UnsavedChangesModal.vue";
import { useEditorStore } from "../stores/editor";
import { useWorkspaceStore } from "../stores/workspace";
import { isMain, useWindowsStore } from "../stores/windows";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: { name: "library" } },
    { path: "/library", name: "library", component: () => import("../pages/library/LibraryPage.vue") },
    { path: "/wizard", name: "wizard", component: () => import("../pages/wizard/WizardPage.vue") },
    {
      path: "/workspace/:id",
      children: [
        { path: "", name: "workspace-ide", component: () => import("../pages/ide/WorkspaceIdePage.vue"), props: true },
        {
          path: "graph",
          name: "event-graph",
          component: () => import("../pages/graph/EventGraphPage.vue"),
          props: true,
        },
        {
          path: "health",
          name: "health",
          component: () => import("../pages/health/HealthPage.vue"),
          // a link opens a card, and a row by its name
          props: (route) => ({ ...route.params, card: route.query.card, name: route.query.name }),
        },
        {
          path: "patch-notes",
          name: "patch-notes",
          component: () => import("../pages/notes/PatchNotesPage.vue"),
          props: true,
        },
        {
          path: "release",
          name: "release",
          component: () => import("../pages/publish/ReleasePage.vue"),
          // a link opens a step: the listing, the version and change note, or review and upload
          props: (route) => ({ ...route.params, step: route.query.step }),
        },
        {
          path: "settings",
          name: "workspace-settings",
          component: () => import("../pages/settings/WorkspaceSettingsPage.vue"),
          // the section is in the query, so the palette and links open the one they name
          props: (route) => ({ ...route.params, section: route.query.section }),
        },
      ],
    },
  ],
});

const unsavedModal = useOverlay().create(UnsavedChangesModal);

router.beforeEach(async (target, previous) => {
  // a window of one thing sends a link to another page to the main window, and the main window one to a page in a
  // window of its own there
  const windows = useWindowsStore();
  const page = `page:${String(target.name)}`;
  const elsewhere = isMain
    ? windows.otherWindows.has(page)
      ? page
      : ""
    : previous.name && target.name !== previous.name
      ? "main"
      : "";
  if (elsewhere) {
    void Events.Emit("window:show", { to: elsewhere, route: target.fullPath, path: "", line: -1, col: 0 });
    return false;
  }
  if (!("id" in target.params)) return;
  // another workspace's files would take the editors' place: the editor windows hand theirs back, then the unsaved
  // edits are saved or dropped, or we stay
  const editor = useEditorStore();
  if (isMain && editor.editingWorkspaceId && target.params.id !== editor.editingWorkspaceId)
    await windows.dock((one) => one.name.startsWith("editor:"));
  if (
    editor.unsaved.size &&
    editor.editingWorkspaceId &&
    target.params.id !== editor.editingWorkspaceId &&
    !(await unsavedModal.open())
  )
    return false;
  useWorkspaceStore().select(target.params.id);
});

export default router;
