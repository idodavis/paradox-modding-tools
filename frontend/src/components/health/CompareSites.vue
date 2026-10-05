<script setup lang="ts">
/**
 * Compare the two (the Health board's button): the replaced definition's file beside the kept one's, what
 * differs marked, opened at the kept definition. Opened through useOverlay.
 */
import { nextTick, onBeforeUnmount, useTemplateRef, watchEffect } from "vue";
import { useQuery } from "@pinia/colada";
import { codeFile, diffView } from "../../lib/code";
import { textQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";

/** One side: a site's file, its origin's name and the definition's 0-based line. */
type Side = { path: string; rel: string; origin: string; line: number; name: string };
const { old, now } = defineProps<{ old: Side; now: Side }>();
const emit = defineEmits<{ close: [] }>();

const store = useWorkspaceStore();
const { data: before, isPending: readingOld } = useQuery(() => textQuery(old.path));
const { data: after, isPending: readingNew } = useQuery(() => textQuery(now.path));
const host = useTemplateRef("host");
const view = diffView();
watchEffect(async () => {
  if (!host.value || before.value === undefined || after.value === undefined) return;
  view.render({
    oldFile: codeFile(old.rel, before.value),
    newFile: codeFile(now.rel, after.value),
    containerWrapper: host.value,
  });
  view.setSelectedLines({ start: now.line + 1, end: now.line + 1, side: "additions" });
  await nextTick();
  host.value.querySelector("diffs-container")?.shadowRoot?.querySelector("[data-selected-line]")?.scrollIntoView({ block: "center" });
});
onBeforeUnmount(() => view.cleanUp());
</script>

<template>
  <UModal title="Compare the two" fullscreen @update:open="emit('close')">
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
            <span class="font-medium">{{ side.name }}</span>
            <span class="truncate font-mono text-dimmed">{{ side.rel }}:{{ side.line + 1 }}</span>
            <span class="grow" />
            <UBadge :label="i ? 'Kept' : 'Replaced'" :color="i ? 'success' : 'neutral'" size="sm" />
          </div>
        </div>
        <p v-if="readingOld || readingNew" class="p-4 text-12 text-dimmed">Reading the two files…</p>
        <p v-else-if="before === undefined || after === undefined" class="p-4 text-12 text-dimmed">
          {{ before === undefined ? old.rel : now.rel }} could not be read.
        </p>
        <div ref="host" class="min-h-0 flex-1 overflow-auto bg-(--bg-0)" />
      </div>
    </template>
  </UModal>
</template>
