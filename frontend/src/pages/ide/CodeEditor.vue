<script setup lang="ts">
/**
 * An editor group's editor: the shown tab's file in a Monaco editor (editor/editor.ts),
 * the hover card over it, and its context menu. A
 * Markdown file shown Rich is the Publish page's editor over the same text.
 */
import { computed, onBeforeUnmount, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem } from "@nuxt/ui";
import * as rpc from "@services/ideservice";
import type { CodeAction, Inspect } from "@services/internal/lsp";
import { createEditor, editorOptions } from "../../editor/editor";
import { applyFix, bytesAt, editText, openFile } from "../../editor/models";
import { showReferences } from "../../editor/search";
import { editorRange } from "../../editor/columns";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { monaco } from "../../lib/monaco";
import { textQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import { useCommandsStore } from "../../stores/commands";
import { MARKDOWN_TOOLBAR } from "../../lib/markdown";
import HoverCard from "./HoverCard.vue";

const { workspaceId, path, group } = defineProps<{ workspaceId: string; path: string; group: number }>();

const router = useRouter();
const editor = useEditorStore();
const palette = useCommandsStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);

const { data: file } = useQuery(() => textQuery(path));
const host = useTemplateRef("host");
const view = shallowRef<ReturnType<typeof createEditor>>();
const textModel = shallowRef<monaco.editor.ITextModel>();

watch(
  [host, file],
  ([el, content]) => {
    if (view.value || !el || !content || content.reject) return;
    textModel.value = openFile(path, content);
    view.value = createEditor(el, textModel.value);
    const { editor: codeEditor } = view.value;
    const { CtrlCmd, Shift, Alt } = monaco.KeyMod;
    const { KeyCode } = monaco;
    codeEditor.addAction({
      id: "pmt.save",
      label: "Save",
      keybindings: [CtrlCmd | KeyCode.KeyS],
      run: () => editor.save([path]),
    });
    codeEditor.addAction({
      id: "pmt.graph",
      label: "Open in Event Graph",
      keybindings: [CtrlCmd | Shift | KeyCode.KeyG],
      run: () => void graph(),
    });
    codeEditor.addAction({
      id: "pmt.findAll",
      label: "Find all references",
      keybindings: [Shift | Alt | KeyCode.F12],
      run: () => void findAll(),
    });
    // the status bar's caret is the shown tab's
    if (editor.focused === group) view.value.reportCaret();
    showPlace();
  },
  { flush: "post" },
);
watch(editorOptions, (options) => view.value?.editor.updateOptions(options));
onBeforeUnmount(() => view.value?.editor.dispose());

/** A place asked of this file in this group: the caret there, scrolled into view. */
function showPlace(): void {
  const place = editor.cursor;
  const codeEditor = view.value?.editor;
  if (!codeEditor || !textModel.value || place?.path !== path || editor.focused !== group) return;
  editor.cursor = null;
  const model = textModel.value;
  const byte = { line: place.line, character: place.col };
  const start = editorRange({ start: byte, end: byte }, (n) =>
    n < model.getLineCount() ? model.getLineContent(n + 1) : undefined,
  ).start;
  const position = { lineNumber: start.line + 1, column: start.character + 1 };
  codeEditor.setPosition(position);
  codeEditor.revealPositionInCenterIfOutsideViewport(position);
  codeEditor.focus();
}
watch(() => editor.cursor, showPlace);

/** The caret as the backend counts, in this editor. */
function caret(): [line: number, col: number] {
  const position = view.value?.editor.getPosition();
  return textModel.value && position ? bytesAt(textModel.value, position) : [0, 0];
}

async function graph(card?: Inspect | null): Promise<void> {
  const [line, col] = caret();
  const word = card ?? (await rpc.Hover(workspaceId, path, line, col));
  if (word?.graphed) void router.push({ name: "event-graph", params: { id: workspaceId }, query: { root: word.word } });
}

/** Every reference to the word at the caret, in the sidebar's Search (the main window's). */
async function findAll(): Promise<void> {
  const [line, col] = caret();
  const card = await rpc.Hover(workspaceId, path, line, col);
  const sites = (await rpc.References(workspaceId, path, line, col)) ?? [];
  showReferences(`References to ${card?.word || "the word"}`, sites);
}

/** Run one of Monaco's actions in this editor. */
function run(action: string): void {
  view.value?.editor.focus();
  view.value?.editor.trigger("menu", action, null);
}

// the context menu's facts at the caret, read when it opens (after Monaco has put the caret where the right-click
// was): the quick fixes and the card
const actions = shallowRef<CodeAction[]>([]);
const card = shallowRef<Inspect | null>(null);
async function onContext(): Promise<void> {
  const [line, col] = caret();
  actions.value = [];
  card.value = null;
  const [fixes, word] = await Promise.all([
    rpc.CodeActions(workspaceId, path, line, col),
    rpc.Hover(workspaceId, path, line, col),
  ]);
  actions.value = fixes ?? [];
  card.value = word;
}

// the Health card a word has rows on: overridden, contested, or defined nowhere
const healthCard = computed(() => {
  const sites = card.value?.sites ?? [];
  if (!card.value?.word) return "";
  if (!sites.length) return "dangling";
  if (sites.length < 2) return "";
  const winner = sites[card.value.winner];
  const replacesGame = sites.some((site, i) => i !== card.value?.winner && site.origin === store.originVanilla);
  return winner && winner.origin !== store.originVanilla && replacesGame ? "overrides" : "contests";
});

/** The selections' text, joined by line breaks. */
function selected(): string {
  const codeEditor = view.value?.editor;
  return (codeEditor?.getSelections() ?? []).map((range) => textModel.value?.getValueInRange(range) ?? "").join("\n");
}

/** Write text over the selections (a paste), or delete them with "" (a cut). */
function write(text: string): void {
  const codeEditor = view.value?.editor;
  codeEditor?.executeEdits(
    "menu",
    (codeEditor.getSelections() ?? []).map((range) => ({ range, text })),
  );
}

const readOnly = computed(() => !!locate(path)?.root.readOnly);
const menu = computed<ContextMenuItem[][]>(() =>
  [
    actions.value.map((action) => ({
      label: action.title,
      icon: "i-lucide-lightbulb",
      onSelect: () => void applyFix(action),
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
      { label: "Find all references", kbds: ["Shift Alt F12"], onSelect: () => void findAll() },
      { label: "Rename", kbds: ["F2"], disabled: readOnly.value, onSelect: () => run("editor.action.rename") },
    ],
    [
      {
        label: "Open in Event Graph",
        kbds: ["Ctrl Shift G"],
        disabled: !card.value?.graphed,
        onSelect: () => void graph(card.value),
      },
      {
        label: "Show in Health",
        disabled: !healthCard.value,
        onSelect: () =>
          void router.push({
            name: "health",
            params: { id: workspaceId },
            query: { card: healthCard.value, name: card.value?.word },
          }),
      },
    ],
    [
      {
        label: "Format document",
        kbds: ["Shift Alt F"],
        disabled: readOnly.value,
        onSelect: () => run("editor.action.formatDocument"),
      },
      { label: "Copy path", onSelect: () => void Clipboard.SetText(path) },
      {
        label: "Cut",
        kbds: ["Ctrl X"],
        disabled: readOnly.value,
        onSelect: () => {
          void Clipboard.SetText(selected());
          write("");
        },
      },
      { label: "Copy", kbds: ["Ctrl C"], onSelect: () => void Clipboard.SetText(selected()) },
      {
        label: "Paste",
        kbds: ["Ctrl V"],
        disabled: readOnly.value,
        onSelect: async () => write(await Clipboard.Text()),
      },
    ],
    [{ label: "Command palette", kbds: ["Ctrl Shift P"], onSelect: () => palette.openPalette() }],
  ].filter((items) => items.length),
);

// the hover card, placed against the word's rectangle
const hover = computed(() => view.value?.hover.value ?? null);
const reference = computed(() => {
  const card = hover.value;
  return card ? { getBoundingClientRect: () => card.anchor } : undefined;
});
const keepFocus = {
  onOpenAutoFocus: (e: Event) => e.preventDefault(),
  onCloseAutoFocus: (e: Event) => e.preventDefault(),
};

// Rich: the Markdown as the Publish page's editor writes it, put into the file's text at each edit
const rich = computed(() => editor.viewOf(path) === "visual");
function writeRich(markdown: string | undefined): void {
  if (markdown !== undefined) editText(path, () => markdown);
}
</script>

<template>
  <!-- the menu takes the card's place: no card shows while it is open, nor one that was about to -->
  <UContextMenu :items="menu" :disabled="rich" :ui="{ content: 'w-79' }" @update:open="(open) => view?.muteHover(open)">
    <div
      ref="host"
      class="min-h-0 flex-1 border-s-12 border-(--bg-0)"
      :class="{ hidden: rich }"
      @contextmenu="onContext"
      @focusin="editor.focused = group"
    />
  </UContextMenu>
  <UEditor
    v-if="rich && textModel"
    v-slot="{ editor: markdownEditor }"
    :model-value="textModel.getValue()"
    content-type="markdown"
    :mention="false"
    :starter-kit="{ heading: { levels: [1, 2, 3] }, underline: false }"
    class="flex min-h-0 flex-1 flex-col"
    :ui="{ content: 'min-h-0 grow overflow-auto bg-(--bg-0) px-6 py-4' }"
    @update:model-value="writeRich"
  >
    <div class="flex h-9 shrink-0 items-center gap-0.5 border-b border-default bg-elevated px-2">
      <UEditorToolbar :editor="markdownEditor" :items="MARKDOWN_TOOLBAR" />
    </div>
  </UEditor>
  <p v-if="file?.reject" class="absolute inset-x-0 top-10 text-center text-12 text-dimmed">{{ file.reject }}</p>
  <UPopover
    :open="!!hover"
    :reference="reference"
    :content="{ side: 'bottom', align: 'start', ...keepFocus }"
    @update:open="(open) => !open && view?.holdHover(false)"
  >
    <template #content>
      <!-- never taller than the room beside the word: a long card scrolls inside itself -->
      <div
        v-if="hover"
        class="max-h-(--reka-popover-content-available-height) overflow-y-auto"
        @mouseenter="view?.holdHover(true)"
        @mouseleave="view?.holdHover(false)"
      >
        <HoverCard :card="hover.card" :problems="hover.problems" />
      </div>
    </template>
  </UPopover>
</template>
