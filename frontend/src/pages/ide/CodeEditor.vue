<!-- A Monaco editor over one Model, with the hover card, merge conflict links and a context menu. -->
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
import type { Place } from "@services/workspace/models";

import {
  compareWithFile,
  compareWithGame,
  findCommand,
  findGameCopy,
  findKbds,
  findOverride,
  overrideEntry,
} from "../../app/commands";
import { copiesQuery, detailQuery, loadQuery, wikiPagesQuery } from "../../queries";
import { useAgentStore } from "../../stores/agent";
import { useEditorStore } from "../../stores/editor";
import { useSearchStore } from "../../stores/search";
import { COMMAND_BUS, useWindowsStore, type WikiLookup } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import HoverCard from "../../ui/HoverCard.vue";
import { markChanges } from "./editor/changes";
import { decorate } from "./editor/conflicts";
import { attachHover } from "./editor/hover";
import type { Model } from "./editor/models";
import { editorOptions } from "./editor/monaco";
import { SHOW_DETAILS } from "./editor/providers";

const { model, active } = defineProps<{ model: Model; active: boolean }>();

/** JUMP_LINES is how many lines the caret must move at once for the history to record the place it left. */
const JUMP_LINES = 10;
/** CARD_CONTENT places the hover card flush against the word and keeps the focus in the editor. */
const CARD_CONTENT: PopoverProps["content"] = {
  side: "top",
  sideOffset: 0,
  align: "start",
  onOpenAutoFocus: (event) => event.preventDefault(),
  onCloseAutoFocus: (event) => event.preventDefault(),
};

const router = useRouter();
const editorStore = useEditorStore();
const agent = useAgentStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const host = useTemplateRef("host");
const view = shallowRef<editor.IStandaloneCodeEditor>();
const hovering = shallowRef<ReturnType<typeof attachHover>>();
const anchor = { getBoundingClientRect: () => hovering.value?.hover.value?.anchor ?? new DOMRect() };
let stopChanges = () => {};
defineExpose({ view });

const { mutate: loadCaret, data: caretFacts } = useMutation({
  mutation: async () => {
    const caret = view.value?.getPosition() ?? { lineNumber: 1, column: 1 };
    const covering = model.problems.filter((problem) => Range.containsPosition(problem.range, caret));
    const [card, fixes, override] = await Promise.all([
      lsp.Hover(model.place.path, caret.lineNumber, caret.column),
      covering.length ? lsp.ListFixes(model.place.path, covering) : null,
      findOverride(model.place, caret.lineNumber, caret.column),
    ]);
    return { card, fixes: fixes ?? [], override };
  },
});
const card = computed(() => caretFacts.value?.card);
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
const { mutate: findFirers } = useMutation({
  mutation: async (subject: Card) => {
    const detail = await loadQuery(detailQuery(subject.node ?? ""));
    const sites = (detail?.firedBy ?? []).map((edge) => edge.site);
    listReferences(`What fires ${subject.title}`, sites);
  },
});
const { data: pages } = useQuery(() => ({
  ...wikiPagesQuery(card.value?.sites?.[0]?.place.path ?? ""),
  enabled: workspace.isOpen() && !card.value?.lexicon && !!card.value?.sites?.length,
}));
/** lookup is the card's wiki look-up: its lexicon row, else its file's first wiki page, else undefined. */
const lookup = computed((): WikiLookup | undefined => {
  if (card.value?.lexicon) return { class: card.value.lexicon, name: card.value.title };
  const title = pages.value?.[0]?.title;
  return title ? { title } : undefined;
});
const { data: copies } = useQuery(() => ({
  ...copiesQuery(model.place.path),
  enabled: model.place.origin !== VANILLA,
}));
const gameCopy = computed(() => findGameCopy(model.place, copies.value));

onMounted(() => {
  if (!host.value) return;
  const codeEditor = editor.create(host.value, {
    ...editorOptions(),
    model: model.model,
    readOnly: model.place.readOnly,
    readOnlyMessage: { value: describeReadOnly(model.place) },
  });
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  codeEditor.setPosition(caret);
  codeEditor.revealPositionInCenterIfOutsideViewport(caret);
  codeEditor.onDidChangeCursorPosition(({ position, reason, source }) => {
    // The history records only a far move the user makes. The app records its own jumps.
    const far = Math.abs(position.lineNumber - model.caret.line) > JUMP_LINES;
    if (active && far && reason === editor.CursorChangeReason.Explicit && source !== "api")
      editorStore.record({ place: model.place, position });
    // eslint-disable-next-line vue/no-mutating-props -- the model is state shared by every group that shows the file
    model.caret = { line: position.lineNumber, column: position.column };
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
  codeEditor.addAction(SHOW_DETAILS);
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

watch(editorOptions, (options) => view.value?.updateOptions(options));
// A range requested when the file was shown is selected once, in the active group's editor. The editor takes focus
// unless Follow the agent opened the file, so the focus stays where the user is typing.
watch(
  [view, () => (active ? editorStore.findRequest(model.place.key) : undefined)],
  ([codeEditor, range]) => {
    if (!codeEditor || !range) return;
    codeEditor.setSelection(range);
    codeEditor.revealRangeInCenterIfOutsideViewport(range);
    if (!editorStore.isFollowed()) codeEditor.focus();
    editorStore.clearRequest();
  },
  { immediate: true },
);

/** listReferences puts the sites in the references list under the given heading and shows it in the main window. */
function listReferences(heading: string, sites: Site[]): void {
  useSearchStore().showReferences(heading, sites);
  windows.sendCommand("editor.findInFiles", "main");
}

/** describeReadOnly returns what the editor says when typing is tried in a read only file. */
function describeReadOnly(place: Place): string {
  if (place.origin === VANILLA) return "Game files are read only. To change this, use Override, or Copy into a mod.";
  return `${workspace.originName(place.origin)} is Steam's copy of a Workshop mod, so PMT keeps it read only. Make a mod for your changes.`;
}

/** keepMovedFocus stops the menu from taking focus back after an item moved it to another text box. */
function keepMovedFocus(event: Event): void {
  const focused = document.activeElement;
  if (focused?.tagName === "TEXTAREA" && !host.value?.contains(focused)) event.preventDefault();
}

/** run triggers one of Monaco's actions in this editor, by its id. */
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

// The name of the entry the caret is in, or "" if the card names none.
const entry = computed(() => card.value?.within?.[0] ?? "");

const groups = computed<ContextMenuItem[][]>(() => [
  [
    ...(caretFacts.value?.override
      ? [
          {
            label: `Override ${entry.value || "this entry"} in a mod`,
            icon: "i-lucide-copy-plus",
            onSelect: () => {
              const caret = view.value?.getPosition() ?? { lineNumber: 1, column: 1 };
              void overrideEntry(model.place, caret.lineNumber, caret.column, entry.value);
            },
          },
        ]
      : []),
    ...(caretFacts.value?.fixes ?? []).map((fix) => ({
      label: fix.title,
      icon: "i-lucide-lightbulb",
      onSelect: () => editorStore.applyFix(fix),
    })),
  ],
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
    {
      label: "Show in Health",
      disabled: !card.value,
      onSelect: () =>
        void router.push({ name: "health", params: { id: workspace.activeId() }, query: { name: card.value?.title } }),
    },
    {
      label: "Look up in the wiki",
      disabled: !lookup.value,
      onSelect: () => lookup.value && windows.lookUp(lookup.value),
    },
    {
      label: `Ask ${agent.current?.name ?? "an agent"} about this`,
      icon: "i-lucide-sparkles",
      onSelect: () => agent.focusPrompt(),
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
  <!-- The menu's trigger takes its child's ref, so the editor's element sits inside the wrapper div. -->
  <UContextMenu
    :items="menu"
    :ui="{ content: 'min-w-79 max-w-130' }"
    :content="{ onCloseAutoFocus: keepMovedFocus }"
    @update:open="(open) => open && loadCaret()"
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
          <!-- A long card scrolls inside itself instead of outgrowing the room beside the word. -->
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
