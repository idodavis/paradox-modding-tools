<!-- A Monaco editor over one model, with the hover card, the merge conflict marks and a context menu. -->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { editor, KeyCode, KeyMod, Range } from "monaco-editor/editor/editor.api";
import type { ContextMenuItem } from "@nuxt/ui";

import * as lsp from "@services/lsp/service";
import type { Fix } from "@services/lsp/models";

import { COMMANDS } from "../../app/commands";
import { loadQuery, placeQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import { decorate } from "./editor/conflicts";
import { attachHover } from "./editor/hover";
import { applyEdits, openModel, type Model } from "./editor/models";
import { editorOptions } from "./editor/monaco";

const { model, active } = defineProps<{ model: Model; active: boolean }>();

const router = useRouter();
const editorStore = useEditorStore();
const workspace = useWorkspaceStore();
const commands = useEventBus<string>("pmt.command");
const host = useTemplateRef("host");
const view = shallowRef<editor.IStandaloneCodeEditor>();
let detachHover = () => {};

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
    const [hovered, locations] = await Promise.all([
      lsp.Hover(model.place.path, caret.lineNumber, caret.column),
      lsp.References(model.place.path, caret.lineNumber, caret.column),
    ]);
    useSearchStore().showReferences(`References to ${hovered?.title ?? "the name"}`, locations ?? []);
    commands.emit("editor.findInFiles");
  },
});
const { mutate: applyFix } = useMutation({
  mutation: async (fix: Fix) => {
    for (const [path, edits] of Object.entries(fix.edits ?? {})) {
      const place = await loadQuery(placeQuery(path));
      if (place) applyEdits(await openModel(place), edits ?? []);
    }
  },
});

onMounted(() => {
  if (!host.value) return;
  const codeEditor = editor.create(host.value, { ...editorOptions(), model: model.model });
  // a file shown again opens where its caret was left
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  codeEditor.setPosition(caret);
  codeEditor.revealPositionInCenterIfOutsideViewport(caret);
  codeEditor.onDidChangeCursorPosition(({ position: caret }) => {
    // eslint-disable-next-line vue/no-mutating-props -- the model is state shared by every group that shows the file
    model.caret = { line: caret.lineNumber, column: caret.column };
  });
  codeEditor.addAction({
    id: "pmt.findAllReferences",
    label: "Find all references",
    keybindings: [KeyMod.Shift | KeyMod.Alt | KeyCode.F12],
    run: () => findAll(),
  });
  detachHover = attachHover(codeEditor);
  view.value = codeEditor;
  decorate(model);
});
onBeforeUnmount(() => {
  detachHover();
  view.value?.dispose();
});

watch(
  () => editorOptions(),
  (options) => view.value?.updateOptions(options),
);
// a position the file was shown at moves the active group's editor there once, and gives it the focus
watch(
  [view, () => (active ? editorStore.findRequest(model.place.key) : undefined)],
  ([codeEditor, wanted]) => {
    if (!codeEditor || !wanted) return;
    codeEditor.setPosition(wanted);
    codeEditor.revealPositionInCenterIfOutsideViewport(wanted);
    codeEditor.focus();
    editorStore.clearRequest();
  },
  { immediate: true },
);

/** Opens Health with its table filtered to the name under the caret. */
async function showInHealth(): Promise<void> {
  await router.push({ name: "health", params: { id: workspace.activeId() }, query: { name: card.value?.title } });
}

/** Runs one of Monaco's own actions in this editor. */
function run(action: string): void {
  view.value?.focus();
  view.value?.trigger("menu", action, null);
}

/** Joins the text of the selections with line breaks. */
function joinSelections(): string {
  return (view.value?.getSelections() ?? []).map((range) => model.model.getValueInRange(range)).join("\n");
}

/** Replaces the selections with a text. An empty text deletes them. */
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
    onSelect: () => applyFix(fix),
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
    { label: "Rename", kbds: ["F2"], disabled: model.place.readOnly, onSelect: () => run("editor.action.rename") },
  ],
  [
    {
      label: "Open in Event Graph",
      kbds: ["Ctrl Shift G"],
      disabled: !card.value?.node,
      onSelect: (event) => void COMMANDS.find((command) => command.id === "editor.openGraph")?.onSelect?.(event),
    },
    { label: "Show in Health", disabled: !card.value, onSelect: () => void showInHealth() },
  ],
  [
    {
      label: "Format document",
      kbds: ["Shift Alt F"],
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
  [{ label: "Command palette", kbds: ["Ctrl Shift P"], onSelect: () => commands.emit("palette.open") }],
]);
const menu = computed(() => groups.value.filter((group) => group.length));
</script>

<template>
  <!-- the menu's trigger takes its child's ref, so the editor's element is the one inside it -->
  <UContextMenu :items="menu" :ui="{ content: 'w-79' }" @update:open="(open) => open && ask()">
    <div class="flex min-h-0 flex-1">
      <div ref="host" class="min-w-0 flex-1 border-s-12 border-(--bg-0)" />
    </div>
  </UContextMenu>
</template>
