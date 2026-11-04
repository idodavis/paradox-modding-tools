/**
 * The tool view store: tells Go which page the user has open and the chip that page offers a prompt, so the agents'
 * tools can see them. It also shows the pages, files, windows and look-ups those tools ask for.
 */
import { useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { defineStore } from "pinia";
import { computed, onScopeDispose, reactive, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { Chip, Show } from "@services/mcp/models";
import * as mcp from "@services/mcp/service";
import { isPage, isWorkspacePage, PAGES } from "../app/pages";
import { drawablesQuery, loadQuery, placeQuery } from "../queries";
import { useDrawStore } from "./draw";
import { useEditorStore } from "./editor";
import { THIS_WINDOW, useWindowsStore } from "./windows";
import { useWorkspaceStore } from "./workspace";

export const useToolViewStore = defineStore("toolView", () => {
  const route = useRoute();
  const router = useRouter();
  const editor = useEditorStore();
  const windows = useWindowsStore();
  const workspace = useWorkspaceStore();
  // the chip each page offers a prompt, by the page's name; the shown page's chip is `chip`
  const chips = reactive(new Map<string, Chip | null>());
  const chip = computed(() => chips.get(String(route.name)) ?? null);
  const chipLeftOut = shallowRef(false);
  // the chip the next prompt takes: the page's, unless the user left it out
  const promptChip = computed(() => (chipLeftOut.value ? null : chip.value));

  watch(chip, () => (chipLeftOut.value = false));
  // sendView tells Go the page, its query and its chip. It runs again when the window gets focus.
  const sendView = (): void =>
    void mcp.SetView({
      page: String(route.name ?? ""),
      query: Object.fromEntries(Object.entries(route.query).map(([key, value]) => [key, String(value ?? "")])),
      chip: chip.value ?? makeChip("", ""),
    });
  watch([() => route.fullPath, chip], sendView, { immediate: true });
  useEventListener(window, "focus", sendView);
  if (THIS_WINDOW === "main") {
    void mcp.SetPages(Object.entries(PAGES).map(([id, page]) => ({ id, title: page.title, help: [...page.help] })));
    onScopeDispose(Events.On("mcp:show", ({ data }: { data: Show }) => void show(data)));
  }

  /** setChip sets the chip a page offers a prompt, or none for null. */
  const setChip = (page: string, next: Chip | null): void => {
    chips.set(page, next);
  };

  /**
   * followFile shows a file an agent's tool call names, at the given line. The source says whether the agent reads or
   * edits the file. It does not change the chip or the focus. It does nothing if Go has no place for the path.
   */
  async function followFile(path: string, line: number, source: "agent" | "edit"): Promise<void> {
    const place = await loadQuery(placeQuery(path)).catch(() => undefined);
    if (!place?.root) return;
    editor.show(place, { lineNumber: line, column: 1 }, source);
  }

  /** show brings to the front what an agent asked to show: a page with its query, a file, a window or a look-up. */
  async function show(request: Show): Promise<void> {
    if (request.page && isPage(request.page)) {
      const name = request.page;
      const params = isWorkspacePage(name) ? { id: workspace.activeId() } : {};
      await router.push({ name, params, query: request.query ?? {} } as Parameters<typeof router.push>[0]);
    }
    if (request.path) {
      const place = await loadQuery(placeQuery(request.path));
      if (!place) return;
      // the editor ends a selection at its line's end
      const start = request.startLine ?? 1;
      const end = { endLineNumber: request.endLine ?? start, endColumn: Number.MAX_SAFE_INTEGER };
      const range = { startLineNumber: start, startColumn: 1, ...end };
      if (request.drawable) {
        // the window's own lines are selected, so the caret is in the window Visual draws
        const entry = (await loadQuery(drawablesQuery(place.path)))?.find(({ name }) => name === request.drawable);
        editor.showVisual(place, entry?.range ?? range);
        useDrawStore().draw(place.path, request.drawable);
        if (request.picks) useDrawStore().pick(request.picks);
      } else {
        editor.show(place, range);
      }
    }
    if (request.lookUp) {
      const { title = "", class: kind = "", name = "" } = request.lookUp;
      windows.lookUp(title ? { title } : { class: kind, name });
    }
  }

  return { chip, chipLeftOut, promptChip, setChip, followFile };
});

/**
 * useChip sets the chip the calling page offers a prompt, and updates it when the source changes. The source returns
 * the chip, or null for none.
 */
export function useChip(source: () => Chip | null): void {
  const toolView = useToolViewStore();
  // a kept-alive page is made on its own route, whose name is the page's
  const page = String(useRoute().name);
  watch(source, (next) => toolView.setChip(page, next), { immediate: true });
}

/** makeChip returns a chip that carries a text, such as an event's id, under a label. */
export function makeChip(label: string, text: string): Chip {
  return { label, path: "", startLine: 0, endLine: 0, text };
}
