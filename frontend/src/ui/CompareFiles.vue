<script setup lang="ts">
/**
 * Two files compared, what differs marked, split or unified, opened at a line of the second: Health's "Compare the
 * two" (the replaced definition's file beside the kept one's) and the IDE's compares. Opened through useOverlay.
 */
import { computed, onBeforeUnmount, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { COMPARE_STYLES, compareStyle, diffView } from "../lib/monaco";
import { textQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import OriginDot from "./OriginDot.vue";

/** One side: a file, its path from its root, its origin and the 0-based line it is opened at. */
type Side = { path: string; rel: string; origin: string; line: number };
const {
  old,
  now,
  title = "Compare",
  tags,
} = defineProps<{ old: Side; now: Side; title?: string; tags?: [string, string] }>();
const emit = defineEmits<{ close: [] }>();

const store = useWorkspaceStore();
const settings = useSettingsStore();
const style = compareStyle;
const { data: oldFile, isPending: readingOld } = useQuery(() => textQuery(old.path));
const { data: newFile, isPending: readingNew } = useQuery(() => textQuery(now.path));
const before = computed(() => oldFile.value?.text);
const after = computed(() => newFile.value?.text);
const host = useTemplateRef("host");
const view = shallowRef<ReturnType<typeof diffView>>();
watch([host, before, after], ([el, a, b]) => {
  if (!el || a === undefined || b === undefined) return;
  if (!view.value) {
    const made = diffView(el, settings.codeFont, style.value === "split", true);
    // opened at no line in particular, it shows the first change
    made.view.onDidUpdateDiff(() => {
      const first = made.view.getLineChanges()?.[0];
      if (!now.line && first)
        made.view.getModifiedEditor().revealLineInCenter(Math.max(first.modifiedStartLineNumber, 1));
    });
    view.value = made;
  }
  view.value.show(
    { text: a, name: old.rel, format: oldFile.value?.format },
    { text: b, name: now.rel, format: newFile.value?.format },
  );
  view.value.view.getModifiedEditor().revealLineInCenter(now.line + 1);
});
watch(style, (next) => view.value?.view.updateOptions({ renderSideBySide: next === "split" }));
onBeforeUnmount(() => view.value?.dispose());
</script>

<template>
  <UModal :title="title" fullscreen @update:open="emit('close')">
    <template #actions>
      <UTabs
        :model-value="style"
        :items="COMPARE_STYLES"
        :content="false"
        size="xs"
        @update:model-value="style = $event === 'unified' ? 'unified' : 'split'"
      />
    </template>
    <template #body>
      <div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-default">
        <div class="grid shrink-0 grid-cols-2 border-b border-default bg-elevated text-12">
          <div
            v-for="(side, i) in [old, now]"
            :key="i"
            class="flex h-8 min-w-0 items-center gap-2 px-2.5 whitespace-nowrap"
            :class="{ 'border-s border-default': i }"
          >
            <OriginDot :color="store.originColor(side.origin)" small />
            <span class="font-medium">{{ store.originName(side.origin) }}</span>
            <span class="truncate font-mono text-dimmed">{{ side.rel }}:{{ side.line + 1 }}</span>
            <span class="grow" />
            <UBadge v-if="tags" :label="tags[i]" :color="i ? 'success' : 'neutral'" size="sm" />
          </div>
        </div>
        <p v-if="readingOld || readingNew" class="p-4 text-12 text-dimmed">Reading the two files…</p>
        <p v-else-if="before === undefined || after === undefined" class="p-4 text-12 text-dimmed">
          {{ before === undefined ? old.rel : now.rel }} could not be read.
        </p>
        <div ref="host" class="min-h-0 flex-1 border-s-12 border-(--bg-0)" />
      </div>
    </template>
  </UModal>
</template>
