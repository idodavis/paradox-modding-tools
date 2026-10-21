/**
 * The page catalogue: each route's name, title, icon and Help text, and for a workspace's pages its tab in the top
 * bar. The router's typed route names come from here, and the Help text is the user's guide.
 */
import type { RouteRecordInfo } from "vue-router";

export type Page = { title: string; help: readonly string[]; icon?: string; workspace?: true; tab?: true };

export const PAGES = {
  library: { title: "Library", icon: "i-lucide-layout-grid", help: [] },
  wizard: { title: "New workspace", help: [] },
  "workspace-ide": { title: "IDE", icon: "i-lucide-code", workspace: true, tab: true, help: [] },
  "event-graph": { title: "Event Graph", icon: "i-lucide-git-branch", workspace: true, tab: true, help: [] },
  health: { title: "Health", icon: "i-lucide-activity", workspace: true, tab: true, help: [] },
  "patch-notes": { title: "Patch Notes", icon: "i-lucide-newspaper", workspace: true, tab: true, help: [] },
  release: { title: "Publish", icon: "i-lucide-upload", workspace: true, tab: true, help: [] },
  "workspace-settings": { title: "Settings", icon: "i-lucide-settings", workspace: true, help: [] },
} as const satisfies Record<string, Page>;

export type PageName = keyof typeof PAGES;
export type WorkspacePage = { [N in PageName]: (typeof PAGES)[N] extends { workspace: true } ? N : never }[PageName];
export type WorkspaceTool = { [N in PageName]: (typeof PAGES)[N] extends { tab: true } ? N : never }[PageName];

declare module "vue-router" {
  interface TypesConfig {
    RouteNamedMap: {
      [N in PageName]: N extends WorkspacePage
        ? RouteRecordInfo<N, string, { id: string }, { id: string }>
        : RouteRecordInfo<N, string, Record<never, never>, Record<never, never>>;
    };
  }
}

/** Whether a route is one of a workspace's pages, which take its id. */
export function isWorkspacePage(name: PageName): name is WorkspacePage {
  throw new Error("stub", { cause: name });
}

/** The top bar's tabs, in order. */
export const WORKSPACE_TOOLS: readonly WorkspaceTool[] = [
  "workspace-ide",
  "event-graph",
  "health",
  "patch-notes",
  "release",
];

/** The page a workspace opens on: its home page if that is shown, else the first page shown. */
export function homeRoute(workspaceId: string, homePage: string, visible: readonly WorkspaceTool[]) {
  throw new Error("stub", { cause: [workspaceId, homePage, visible] });
}
