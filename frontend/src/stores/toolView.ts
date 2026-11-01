/**
 * PMT's view for its tools: the page the user has in front of them and the chip that page offers a prompt, which Go
 * sends on to the agents' tools, and the pages, files, windows and look-ups those tools ask PMT to show.
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
  // what each page offers a prompt, by the page's name; the chip is the page shown's, and the user can leave it out
  const chips = reactive(new Map<string, Chip | null>());
  const chip = computed(() => chips.get(String(route.name)) ?? null);
  const chipLeftOut = shallowRef(false);
  // the chip the next prompt takes: the page's, unless the user left it out
  const promptChip = computed(() => (chipLeftOut.value ? null : chip.value));

  watch(chip, () => (chipLeftOut.value = false));
  // Go's tools say what the user has in front of them: the page and its chip, sent again when the window has focus
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

  /** setChip keeps what a page offers a prompt, or null for nothing. */
  const setChip = (page: string, next: Chip | null): void => {
    chips.set(page, next);
  };

  /**
   * followFile opens a file an agent's tool call names at a line, in the main window's IDE, as a file the agent reads
   * or one it edits. The file is the agent's, so the chip stays the user's own and the focus stays where it is.
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
 * useChip keeps what the calling page offers a prompt, which the prompt takes while the page is shown. The source
 * gives the chip, or null for none.
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
