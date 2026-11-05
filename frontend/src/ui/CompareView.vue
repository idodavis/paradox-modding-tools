<!-- Two texts in a diff editor, with a label above each and controls in a header row. -->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useLocalStorage } from "@vueuse/core";
import { editor } from "monaco-editor/editor/editor.api";

import { plural } from "../lib/format";
import { useSettingsStore } from "../stores/settings";

const { left, right, language, leftLabel, rightLabel } = defineProps<{
  left: string;
  right: string;
  language: string;
  leftLabel: string;
  rightLabel: string;
}>();
const emit = defineEmits<{ changes: [added: number, removed: number] }>();

const STYLES = [
  { label: "Split", value: "split" },
  { label: "Unified", value: "unified" },
];

const style = useLocalStorage("pmt.compare.style", "split");
const all = useLocalStorage("pmt.compare.all", false);
const changes = shallowRef(0);
const split = computed(() => style.value !== "unified");
const labels = computed(() =>
  split.value
    ? [
        { text: leftLabel, icon: "i-lucide-git-commit-horizontal", class: "border-e border-default" },
        { text: rightLabel, icon: "i-lucide-pencil", class: "" },
      ]
    : [{ text: `${leftLabel} → ${rightLabel}`, icon: "i-lucide-git-commit-horizontal", class: "" }],
);
const host = useTemplateRef("host");
const original = editor.createModel(left, language);
const modified = editor.createModel(right, language);
let view: editor.IStandaloneDiffEditor | undefined;

onMounted(() => {
  if (!host.value) return;
  view = editor.createDiffEditor(host.value, {
    ...useSettingsStore().codeFont(),
    readOnly: true,
    automaticLayout: true,
    renderSideBySide: split.value,
    // the lane of the + and − signs, wide enough to keep them off the line numbers and the text
    lineDecorationsWidth: "3ch",
    ignoreTrimWhitespace: false,
    useInlineViewWhenSpaceIsLimited: false,
    hideUnchangedRegions: { enabled: !all.value },
    renderOverviewRuler: false,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    stickyScroll: { enabled: false },
    contextmenu: false,
  });
  view.setModel({ original, modified });
  view.onDidUpdateDiff(() => {
    const lines = view?.getLineChanges() ?? [];
    changes.value = lines.length;
    const count = (start: number, end: number) => (end ? end - start + 1 : 0);
    // An empty text has no lines, though Monaco gives its model one.
    emit(
      "changes",
      right ? lines.reduce((sum, line) => sum + count(line.modifiedStartLineNumber, line.modifiedEndLineNumber), 0) : 0,
      left ? lines.reduce((sum, line) => sum + count(line.originalStartLineNumber, line.originalEndLineNumber), 0) : 0,
    );
  });
});

// setValue loses the marks Monaco keeps of the folded regions, so a new text compares again.
watch(
  () => left,
  (text) => {
    original.setValue(text);
    compareAgain();
  },
);
watch(
  () => right,
  (text) => {
    modified.setValue(text);
    compareAgain();
  },
);
watch(
  () => language,
  (id) => {
    editor.setModelLanguage(original, id);
    editor.setModelLanguage(modified, id);
  },
);
watch(split, (sideBySide) => view?.updateOptions({ renderSideBySide: sideBySide }));
watch(all, (shown) => {
  view?.updateOptions({ hideUnchangedRegions: { enabled: !shown } });
  if (!shown) compareAgain();
});

onBeforeUnmount(() => {
  view?.dispose();
  original.dispose();
  modified.dispose();
});

/** compareAgain compares the two texts anew, folding the unchanged regions again. */
function compareAgain(): void {
  view?.setModel({ original, modified });
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div
      class="flex h-7.5 shrink-0 items-center gap-2 border-b border-default bg-elevated ps-3.5 pe-2.5 text-12 whitespace-nowrap text-dimmed"
    >
      <slot name="header" />
      <span class="grow" />
      <UButton
        icon="i-lucide-chevron-up"
        variant="ghost"
        size="xs"
        square
        :disabled="!changes"
        aria-label="Previous change"
        @click="view?.goToDiff('previous')"
      />
      <UButton
        icon="i-lucide-chevron-down"
        variant="ghost"
        size="xs"
        square
        :disabled="!changes"
        aria-label="Next change"
        @click="view?.goToDiff('next')"
      />
      <span class="tabular-nums">{{ plural(changes, "change") }}</span>
      <USwitch v-model="all" label="Show all lines" size="xs" :ui="{ label: 'text-11 font-normal text-dimmed' }" />
      <UTabs
        :model-value="style"
        :items="STYLES"
        :content="false"
        size="xs"
        :ui="{ list: 'h-5', trigger: 'h-4 px-2 text-11' }"
        @update:model-value="style = String($event)"
      />
    </div>
    <div class="flex h-7 shrink-0 border-b border-default bg-muted text-11 whitespace-nowrap text-dimmed">
      <span
        v-for="label in labels"
        :key="label.text"
        class="flex min-w-0 flex-1 items-center gap-1.5 px-3"
        :class="label.class"
      >
        <UIcon :name="label.icon" class="size-3.5 shrink-0" />
        <span class="truncate">{{ label.text }}</span>
      </span>
    </div>
    <div ref="host" class="min-h-0 flex-1 border-s-12 border-(--bg-0) bg-(--bg-0)" />
  </div>
</template>
