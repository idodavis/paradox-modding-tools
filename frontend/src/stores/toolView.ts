/** The tool view store: tells Go the open page and its chip for the agents' tools, and shows what the tools ask for. */
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
  const chips = reactive(new Map<string, Chip | null>());
  const chip = computed(() => chips.get(String(route.name)) ?? null);
  const chipLeftOut = shallowRef(false);
  const promptChip = computed(() => (chipLeftOut.value ? null : chip.value));

  watch(chip, () => (chipLeftOut.value = false));
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

  /** followFile shows a file an agent's tool call names, at the given line, without changing the chip or the focus. */
  async function followFile(path: string, line: number, source: "agent" | "edit"): Promise<void> {
    const place = await loadQuery(placeQuery(path)).catch(() => undefined);
    if (!place?.root) return;
    editor.show(place, { lineNumber: line, column: 1 }, source);
  }

  /** show brings to the front what an agent asked to show. */
  async function show(request: Show): Promise<void> {
    if (request.page && isPage(request.page)) {
      const name = request.page;
      const params = isWorkspacePage(name) ? { id: workspace.activeId() } : {};
      await router.push({ name, params, query: request.query ?? {} } as Parameters<typeof router.push>[0]);
    }
    if (request.path) {
      const place = await loadQuery(placeQuery(request.path));
      if (!place) return;
      // The editor ends a selection at its line's end.
      const start = request.startLine ?? 1;
      const end = { endLineNumber: request.endLine ?? start, endColumn: Number.MAX_SAFE_INTEGER };
      const range = { startLineNumber: start, startColumn: 1, ...end };
      if (request.drawable) {
        // The window's own lines are selected, so the caret is in the window Visual draws.
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

/** useChip sets the chip the calling page offers a prompt, and updates it when the source changes. */
export function useChip(source: () => Chip | null): void {
  const toolView = useToolViewStore();
  // A kept-alive page is made on its own route, whose name is the page's.
  const page = String(useRoute().name);
  watch(source, (next) => toolView.setChip(page, next), { immediate: true });
}

/** makeChip returns a chip that carries a text, such as an event's id, under a label. */
export function makeChip(label: string, text: string): Chip {
  return { label, path: "", startLine: 0, endLine: 0, text };
}
