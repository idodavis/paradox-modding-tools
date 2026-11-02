<!--
  A Monaco editor over one model, with the hover card, the merge conflict marks and a context menu. It exposes the
  editor it creates.
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { editor, KeyCode, KeyMod, Range } from "monaco-editor/editor/editor.api";
import type { ContextMenuItem, PopoverProps } from "@nuxt/ui";

import * as lsp from "@services/lsp/service";
import type { Card, Site } from "@services/lsp/models";

import { compareWithFile, compareWithGame, findCommand, findGameCopy, findKbds } from "../../app/commands";
import { copiesQuery, detailQuery, loadQuery, wikiPagesQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useSearchStore } from "../../stores/search";
import { COMMAND_BUS, useWindowsStore, type WikiLookup } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import HoverCard from "../../ui/HoverCard.vue";
import { markChanges } from "./editor/changes";
import { decorate } from "./editor/conflicts";
import { attachHover } from "./editor/hover";
import type { Model } from "./editor/models";
import { editorOptions } from "./editor/monaco";

const { model, active } = defineProps<{ model: Model; active: boolean }>();

/** How many lines the caret must move in one go for the history to keep the place it left. */
const JUMP_LINES = 10;
/**
 * Where the hover card sits: flush on the word, so the pointer crosses no other line on its way to the card. The focus
 * stays in the editor as the card opens and closes.
 */
const CARD_CONTENT: PopoverProps["content"] = {
  side: "top",
  sideOffset: 0,
  align: "start",
  onOpenAutoFocus: (event) => event.preventDefault(),
  onCloseAutoFocus: (event) => event.preventDefault(),
};

const router = useRouter();
const editorStore = useEditorStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const host = useTemplateRef("host");
const view = shallowRef<editor.IStandaloneCodeEditor>();
const hovering = shallowRef<ReturnType<typeof attachHover>>();
// the hover card is anchored on the rectangle of the word the pointer rests on
const anchor = { getBoundingClientRect: () => hovering.value?.hover.value?.anchor ?? new DOMRect() };
let stopChanges = () => {};
defineExpose({ view });

const { mutate: ask, data: asked } = useMutation({
  mutation: async () => {
    const caret = view.value?.getPosition() ?? { lineNumber: 1, column: 1 };
    const covering = model.problems.filter((problem) => Range.containsPosition(problem.range, caret));
    const [card, fixes] = await Promise.all([
      lsp.Hover(model.place.path, caret.lineNumber, caret.column),
      covering.length ? lsp.ListFixes(model.place.path, covering) : null,
    ]);
    return { card, fixes: fixes ?? [] };
  },
});
const card = computed(() => asked.value?.card);
const { mutate: findAll } = useMutation({
  mutation: async () => {
    const caret = view.value?.getPosition() ?? { lineNumber: 1, column: 1 };
    const [hovered, sites] = await Promise.all([
      lsp.Hover(model.place.path, caret.lineNumber, caret.column),
      lsp.References(model.place.path, caret.lineNumber, caret.column),
    ]);
    listReferences(`References to ${hovered?.title ?? "the name"}`, sites ?? []);
  },
});
// the places that fire a name the Event Graph draws, from the name's detail there
const { mutate: findFirers } = useMutation({
  mutation: async (subject: Card) => {
    const detail = await loadQuery(detailQuery(subject.node ?? ""));
    const sites = (detail?.firedBy ?? []).map((edge) => edge.site);
    listReferences(`What fires ${subject.title}`, sites);
  },
});
// a definition the lexicon does not list looks up the first wiki page of the file it is defined in
const { data: pages } = useQuery(() => ({
  ...wikiPagesQuery(card.value?.sites?.[0]?.place.path ?? ""),
  enabled: workspace.isOpen() && !card.value?.lexicon && !!card.value?.sites?.length,
}));
/** The card's look-up: its lexicon row, else its file's first wiki page, else none. */
const lookup = computed((): WikiLookup | undefined => {
  if (card.value?.lexicon) return { class: card.value.lexicon, name: card.value.title };
  const title = pages.value?.[0]?.title;
  return title ? { title } : undefined;
});
// the menu offers the compare with the game's copy only for a mod's file the game has too
const { data: copies } = useQuery(() => ({ ...copiesQuery(model.place.path), enabled: !model.place.readOnly }));
const gameCopy = computed(() => findGameCopy(model.place, copies.value));

onMounted(() => {
  if (!host.value) return;
  const codeEditor = editor.create(host.value, { ...editorOptions(), model: model.model });
  // a file shown again opens where its caret was left
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  codeEditor.setPosition(caret);
  codeEditor.revealPositionInCenterIfOutsideViewport(caret);
  codeEditor.onDidChangeCursorPosition(({ position: caret, reason, source }) => {
    // the user moving the caret far in one go is a jump the history keeps; a move the app makes is kept by its jump
    const far = Math.abs(caret.lineNumber - model.caret.line) > JUMP_LINES;
    if (active && far && reason === editor.CursorChangeReason.Explicit && source !== "api")
      editorStore.record({ place: model.place, position: caret });
    // eslint-disable-next-line vue/no-mutating-props -- the model is state shared by every group that shows the file
    model.caret = { line: caret.lineNumber, column: caret.column };
  });
  codeEditor.onDidChangeCursorSelection(({ selection }) => {
    // eslint-disable-next-line vue/no-mutating-props -- the model is state shared by every group that shows the file
    model.lines = selection.isEmpty() ? null : { start: selection.startLineNumber, end: selection.endLineNumber };
  });
  codeEditor.addAction({
    id: "pmt.findAllReferences",
    label: "Find all references",
    keybindings: [KeyMod.Shift | KeyMod.Alt | KeyCode.F12],
    run: () => findAll(),
  });
  hovering.value = attachHover(codeEditor, model);
  view.value = codeEditor;
  decorate(model);
  stopChanges = markChanges(codeEditor, model);
});
onBeforeUnmount(() => {
  hovering.value?.detach();
  stopChanges();
  view.value?.dispose();
});

watch(
  () => editorOptions(),
  (options) => view.value?.updateOptions(options),
);
// a range the file was shown with is selected once in the active group's editor, which takes the focus unless Follow
// the agent showed the file: the user's focus stays where they are typing
watch(
  [view, () => (active ? editorStore.findRequest(model.place.key) : undefined)],
  ([codeEditor, wanted]) => {
    if (!codeEditor || !wanted) return;
    codeEditor.setSelection(wanted);
    codeEditor.revealRangeInCenterIfOutsideViewport(wanted);
    if (!editorStore.isFollowed()) codeEditor.focus();
    editorStore.clearRequest();
  },
  { immediate: true },
);

/** showInHealth opens Health with its table filtered to the name under the caret. */
async function showInHealth(): Promise<void> {
  await router.push({ name: "health", params: { id: workspace.activeId() }, query: { name: card.value?.title } });
}

/**
 * listReferences lists places in the sidebar's references under the given heading, which every window's list shows. It
 * also shows the main window's sidebar search.
 */
function listReferences(heading: string, sites: Site[]): void {
  useSearchStore().showReferences(heading, sites);
  windows.sendCommand("editor.findInFiles", "main");
}

/** keepMovedFocus keeps the focus in the editor a row of the menu moved it to, such as the file a fix opened. */
function keepMovedFocus(event: Event): void {
  const focused = document.activeElement;
  if (focused?.closest(".monaco-editor") && !host.value?.contains(focused)) event.preventDefault();
}

/** run runs one of Monaco's own actions in this editor. */
function run(action: string): void {
  view.value?.focus();
  view.value?.trigger("menu", action, null);
}

/** joinSelections joins the text of the selections with line breaks. */
function joinSelections(): string {
  return (view.value?.getSelections() ?? []).map((range) => model.model.getValueInRange(range)).join("\n");
}

/** replaceSelections replaces the selections with the given text. An empty text deletes them. */
function replaceSelections(text: string): void {
  view.value?.executeEdits(
    "menu",
    (view.value.getSelections() ?? []).map((range) => ({ range, text })),
  );
}

const groups = computed<ContextMenuItem[][]>(() => [
  (asked.value?.fixes ?? []).map((fix) => ({
    label: fix.title,
    icon: "i-lucide-lightbulb",
    onSelect: () => editorStore.applyFix(fix),
  })),
  [
    { label: "Go to definition", kbds: ["F12"], onSelect: () => run("editor.action.revealDefinition") },
    { label: "Go to references", kbds: ["Shift F12"], onSelect: () => run("editor.action.goToReferences") },
    {
      label: "Peek",
      children: [
        { label: "Peek definition", kbds: ["Alt F12"], onSelect: () => run("editor.action.peekDefinition") },
        { label: "Peek references", onSelect: () => run("editor.action.referenceSearch.trigger") },
      ],
    },
    { label: "Find all references", kbds: ["Shift Alt F12"], onSelect: () => findAll() },
    ...(card.value?.node
      ? [{ label: "Find what fires this", onSelect: () => card.value && findFirers(card.value) }]
      : []),
    { label: "Rename", kbds: ["F2"], disabled: model.place.readOnly, onSelect: () => run("editor.action.rename") },
  ],
  [
    {
      label: "Open in Event Graph",
      kbds: findKbds("editor.openGraph"),
      disabled: !card.value?.node,
      onSelect: (event) => void findCommand("editor.openGraph")?.onSelect?.(event),
    },
    { label: "Show in Health", disabled: !card.value, onSelect: () => void showInHealth() },
    {
      label: "Look up in the wiki",
      disabled: !lookup.value,
      onSelect: () => lookup.value && windows.lookUp(lookup.value),
    },
  ],
  [
    ...(gameCopy.value
      ? [{ label: "Compare with the game's copy", onSelect: () => compareWithGame(model.place) }]
      : []),
    { label: "Compare with another file…", onSelect: () => compareWithFile(model.place) },
  ],
  [
    {
      label: "Format document",
      kbds: findKbds("editor.format"),
      disabled: model.place.readOnly,
      onSelect: () => run("editor.action.formatDocument"),
    },
    { label: "Copy path", onSelect: () => void Clipboard.SetText(model.place.path) },
    {
      label: "Cut",
      kbds: ["Ctrl X"],
      disabled: model.place.readOnly,
      onSelect: () => {
        void Clipboard.SetText(joinSelections());
        replaceSelections("");
      },
    },
    { label: "Copy", kbds: ["Ctrl C"], onSelect: () => void Clipboard.SetText(joinSelections()) },
    {
      label: "Paste",
      kbds: ["Ctrl V"],
      disabled: model.place.readOnly,
      onSelect: () => void Clipboard.Text().then(replaceSelections),
    },
  ],
  [{ label: "Command palette", kbds: findKbds("palette.open"), onSelect: () => commandBus.emit("palette.open") }],
]);
const menu = computed(() => groups.value.filter((group) => group.length));
</script>

<template>
  <!-- the menu's trigger takes its child's ref, so the editor's element is the one inside it -->
  <UContextMenu
    :items="menu"
    :ui="{ content: 'min-w-79 max-w-130' }"
    :content="{ onCloseAutoFocus: keepMovedFocus }"
    @update:open="(open) => open && ask()"
  >
    <div class="flex min-h-0 flex-1">
      <div ref="host" class="min-w-0 flex-1 border-s-12 border-(--bg-0)" />
      <UPopover
        :open="!!hovering?.hover.value"
        :reference="anchor"
        :content="CARD_CONTENT"
        @update:open="(open) => open || hovering?.close()"
      >
        <template #content>
          <!-- never taller than the room beside the word: a long card scrolls inside itself -->
          <div
            v-if="hovering?.hover.value"
            class="max-h-(--reka-popover-content-available-height) overflow-y-auto"
            @mouseenter="hovering.hold(true)"
            @mouseleave="hovering.hold(false)"
          >
            <HoverCard :card="hovering.hover.value.card" :problems="hovering.hover.value.problems" />
          </div>
        </template>
      </UPopover>
    </div>
  </UContextMenu>
</template>
