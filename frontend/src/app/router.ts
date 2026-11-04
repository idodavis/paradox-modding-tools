/** The router: one route per page of the catalog, loaded lazily, with one guard that sets the active workspace. */
import {
  createRouter,
  createWebHashHistory,
  type RouteLocationNormalized,
  type RouteRecordRaw,
  type Router,
} from "vue-router";

import { saveUnsaved } from "../pages/ide/editor/models";
import { THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import { WORKSPACE_TOOLS, type PageName } from "./pages";

const ROUTES = {
  library: { path: "/library", component: () => import("../pages/library/LibraryPage.vue") },
  "new-mod": { path: "/new-mod", component: () => import("../pages/newmod/NewModPage.vue") },
  wizard: { path: "/wizard", component: () => import("../pages/wizard/WizardPage.vue") },
  "workspace-ide": { path: "", component: () => import("../pages/ide/WorkspaceIdePage.vue") },
  "event-graph": { path: "graph", component: () => import("../pages/graph/EventGraphPage.vue") },
  health: { path: "health", component: () => import("../pages/health/HealthPage.vue") },
  "patch-notes": { path: "patch-notes", component: () => import("../pages/notes/PatchNotesPage.vue") },
  release: { path: "release", component: () => import("../pages/publish/ReleasePage.vue") },
} satisfies Record<PageName, Pick<RouteRecordRaw, "path" | "component">>;

/** router is the app's router. Its guard writes the active workspace id. */
export const router: Router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: { name: "library" } },
    makeRoute("library"),
    makeRoute("new-mod"),
    makeRoute("wizard"),
    { path: "/workspace/:id", children: WORKSPACE_TOOLS.map(makeRoute) },
  ],
});

router.beforeEach(async (target, previous) => {
  if (THIS_WINDOW !== "main" && previous.name && target.name !== previous.name) {
    useWindowsStore().openPage(target.fullPath);
    return false;
  }
  const params = target.params;
  if (!("id" in params) || typeof params.id !== "string") return;
  const workspace = useWorkspaceStore();
  if (params.id === workspace.activeId() && workspace.isOpen()) return;
  if (params.id !== workspace.activeId() && !(await saveUnsaved("Switching workspace"))) return false;
  workspace.open(params.id);
});

/** makeRoute makes the route of a page. The page gets the route's params and query as props. */
function makeRoute(name: PageName): RouteRecordRaw {
  return { name, path: ROUTES[name].path, component: ROUTES[name].component, props: resolveProps };
}

/** resolveProps returns the props a page takes: the workspace's id and whatever the query names. */
function resolveProps(route: RouteLocationNormalized): Record<string, unknown> {
  return { ...route.params, ...route.query };
}
