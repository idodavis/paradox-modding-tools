<!-- Two texts in a diff editor, with a toolbar above it and a chip above each side. -->
<script lang="ts">
import { editor } from "monaco-editor/editor/editor.api";

/** DIFF_OPTIONS holds the options every diff editor in the app shares. */
export const DIFF_OPTIONS: editor.IDiffEditorConstructionOptions = {
  originalEditable: false,
  renderIndicators: true,
  // the lane of the + and − signs, wide enough to keep them off the numbers
  // and the text
  lineDecorationsWidth: "3ch",
  ignoreTrimWhitespace: false,
  useInlineViewWhenSpaceIsLimited: false,
};
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useLocalStorage } from "@vueuse/core";

import { plural } from "../lib/format";
import { useSettingsStore } from "../stores/settings";

const { left, right, language, leftLabel, rightLabel } = defineProps<{
  left: string;
  right: string;
  language: string;
  leftLabel: string;
  rightLabel: string;
}>();

const STYLES = [
  { label: "Split", value: "split" },
  { label: "Unified", value: "unified" },
];

const style = useLocalStorage("pmt.compare.style", "split");
const all = useLocalStorage("pmt.compare.all", false);
const changes = shallowRef(0);
const additions = shallowRef(0);
const removals = shallowRef(0);
const split = computed(() => style.value !== "unified");
const host = useTemplateRef("host");
const original = editor.createModel(left, language);
const modified = editor.createModel(right, language);
let view: editor.IStandaloneDiffEditor | undefined;

onMounted(() => {
  if (!host.value) return;
  view = editor.createDiffEditor(host.value, {
    ...useSettingsStore().codeFont(),
    ...DIFF_OPTIONS,
    readOnly: true,
    automaticLayout: true,
    renderSideBySide: split.value,
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
    additions.value = right
      ? lines.reduce((sum, line) => sum + count(line.modifiedStartLineNumber, line.modifiedEndLineNumber), 0)
      : 0;
    removals.value = left
      ? lines.reduce((sum, line) => sum + count(line.originalStartLineNumber, line.originalEndLineNumber), 0)
      : 0;
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
      class="flex h-9 shrink-0 items-center gap-3 border-b border-default bg-elevated ps-3.5 pe-2.5 text-12 whitespace-nowrap text-dimmed"
    >
      <slot name="header" />
      <span class="grow" />
      <div class="flex items-center gap-2">
        <UFieldGroup size="xs">
          <UButton
            icon="i-lucide-chevron-up"
            color="neutral"
            variant="subtle"
            :disabled="!changes"
            aria-label="Previous change"
            @click="view?.goToDiff('previous')"
          />
          <UButton
            icon="i-lucide-chevron-down"
            color="neutral"
            variant="subtle"
            :disabled="!changes"
            aria-label="Next change"
            @click="view?.goToDiff('next')"
          />
        </UFieldGroup>
        <span class="tabular-nums">{{ plural(changes, "change") }}</span>
        <span class="font-mono text-success tabular-nums">+{{ additions }}</span>
        <span class="font-mono text-error tabular-nums">−{{ removals }}</span>
      </div>
      <USeparator orientation="vertical" class="h-4" />
      <USwitch v-model="all" label="Show all lines" size="xs" :ui="{ label: 'text-12 font-normal text-dimmed' }" />
      <USeparator orientation="vertical" class="h-4" />
      <UTabs
        :model-value="style"
        :items="STYLES"
        :content="false"
        size="xs"
        :ui="{ list: 'h-6', trigger: 'h-5 px-2.5 text-12' }"
        @update:model-value="style = String($event)"
      />
    </div>
    <div class="flex h-8 shrink-0 items-center border-b border-default bg-muted whitespace-nowrap">
      <div class="flex min-w-0 items-center gap-2 ps-3" :class="split ? 'flex-1 border-e border-default pe-3' : 'pe-2'">
        <UBadge
          :label="leftLabel"
          icon="i-lucide-minus"
          color="error"
          variant="subtle"
          size="md"
          class="min-w-0"
          :ui="{ label: 'truncate' }"
        />
        <UIcon v-if="!split" name="i-lucide-arrow-right" class="size-3.5 shrink-0 text-dimmed" />
      </div>
      <div class="flex min-w-0 items-center" :class="split ? 'flex-1 px-3' : 'pe-3'">
        <UBadge
          :label="rightLabel"
          icon="i-lucide-plus"
          color="success"
          variant="subtle"
          size="md"
          class="min-w-0"
          :ui="{ label: 'truncate' }"
        />
      </div>
    </div>
    <div ref="host" class="min-h-0 flex-1 bg-(--bg-0)" />
  </div>
</template>
