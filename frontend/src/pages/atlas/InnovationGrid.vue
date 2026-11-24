<!-- CK3's innovations, which require nothing of each other: a grid of era by group, with what they unlock as chips. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useFullscreen } from "@vueuse/core";

import type { Graph, Node } from "@services/atlas/models";
import { describeCount, plural, toSentence } from "../../lib/format";
import StatusFacts from "../../ui/StatusFacts.vue";
import NodePicture, { labelOf } from "./NodePicture.vue";
import PaintingBand from "./PaintingBand.vue";

const { graph, title } = defineProps<{
  graph: Graph;
  /** What the game calls its technologies, as the catalog's tab does. */
  title: string;
}>();
const emit = defineEmits<{ ask: [id: string] }>();

const page = useTemplateRef("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);
const technologies = computed(() => graph.nodes ?? []);
const eras = computed(() => graph.eras ?? []);
const noun = computed(() => technologies.value[0]?.what ?? "");
const columns = computed(() => ({ gridTemplateColumns: `repeat(${eras.value.length}, minmax(0, 1fr))` }));
/** The technologies of each group in each era. */
const cells = computed(() => Map.groupBy(technologies.value, (technology) => toCell(technology.group, technology.era)));
/** The database of what the lit cards unlock, "" for none lit. */
const lit = shallowRef("");
const litCount = computed(() => (lit.value ? technologies.value.filter(isLit).length : 0));
/** The band that is open, one at a time: the subject's at first. */
const open = shallowRef("");
watch(
  () => technologies.value.find((technology) => technology.id === graph.root)?.group,
  (group) => (open.value = group ?? ""),
  { immediate: true },
);
const facts = computed(() => [
  {
    label: toSentence(title),
    value: `${describeCount(technologies.value.length)} in ${plural(eras.value.length, "era")} and ${plural(graph.trees?.length ?? 0, "group")}`,
  },
]);

function toCell(group = "", era = ""): string {
  return `${group}|${era}`;
}

function isLit(technology: Node): boolean {
  return !!technology.unlocks?.some((unlocked) => unlocked.database === lit.value);
}

/** describeEra returns an era's line: the year it starts in, where the game writes one, then how many it holds. */
function describeEra(era: Node): string {
  const from = era.year === "0" ? "from the start" : era.year ? `from ${era.year}` : "";
  return [from, plural(era.count, noun.value)].filter(Boolean).join(" · ");
}
</script>

<template>
  <div ref="page" class="flex h-full min-h-0 w-full min-w-0 flex-col bg-(--bg-0)">
    <div
      class="flex h-11 shrink-0 items-center gap-2 overflow-x-auto border-b border-default bg-muted px-3 whitespace-nowrap"
    >
      <h2 class="text-14 font-semibold text-highlighted">{{ toSentence(title) }}</h2>
      <span class="text-12 text-dimmed">by era and group</span>
      <template v-if="graph.unlocks?.length">
        <span class="ps-3 text-12 text-dimmed">Unlocks</span>
        <UButton
          v-for="unlocked in graph.unlocks"
          :key="unlocked.id"
          :label="labelOf(unlocked)"
          :color="lit === unlocked.id ? 'secondary' : 'neutral'"
          :variant="lit === unlocked.id ? 'soft' : 'outline'"
          size="xs"
          class="rounded-full"
          @click="lit = lit === unlocked.id ? '' : unlocked.id"
        >
          <template #trailing>
            <small class="text-11 font-normal tabular-nums opacity-70">{{ describeCount(unlocked.count) }}</small>
          </template>
        </UButton>
        <UButton v-if="lit" label="Clear" color="neutral" variant="ghost" size="xs" @click="lit = ''" />
      </template>
      <span class="grow" />
      <span class="text-12 text-dimmed tabular-nums">
        {{ plural(technologies.length, noun) }}<template v-if="litCount">, {{ describeCount(litCount) }} lit</template>
      </span>
      <UButton
        :icon="isFullscreen ? 'i-lucide-minimize' : 'i-lucide-maximize'"
        color="neutral"
        variant="ghost"
        size="sm"
        square
        :aria-label="isFullscreen ? 'Leave full screen' : 'Full screen'"
        @click="toggleFullscreen"
      />
      <slot />
    </div>
    <div class="grid h-23 shrink-0 border-b border-default" :style="columns">
      <PaintingBand
        v-for="era in eras"
        :key="era.id"
        :painting="era.painting"
        class="flex flex-col justify-center gap-0.5 border-s border-default px-4 first:border-s-0"
      >
        <b class="truncate text-14 text-highlighted">{{ labelOf(era) }}</b>
        <small class="truncate text-11.5 text-muted">{{ describeEra(era) }}</small>
      </PaintingBand>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <section v-for="group in graph.trees" :key="group.id" class="border-b border-default">
        <PaintingBand :painting="group.painting" class="bg-muted">
          <button
            type="button"
            class="flex w-full cursor-pointer items-center gap-2.5 px-3.5 text-start text-12 whitespace-nowrap text-dimmed"
            :class="open === group.id ? 'h-13' : 'h-11'"
            :aria-expanded="open === group.id"
            @click="open = open === group.id ? '' : group.id"
          >
            <UIcon
              :name="open === group.id ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              class="size-4 shrink-0"
            />
            <b class="text-13 font-semibold text-highlighted">{{ group.title }}</b>
            <span class="text-muted tabular-nums">{{ plural(group.count, noun) }}</span>
          </button>
        </PaintingBand>
        <div v-if="open === group.id" class="grid border-t border-default bg-default/60" :style="columns">
          <div
            v-for="era in eras"
            :key="era.id"
            class="flex min-w-0 flex-col gap-1.25 border-s border-default p-2 first:border-s-0"
          >
            <button
              v-for="technology in cells.get(toCell(group.id, era.id))"
              :key="technology.id"
              type="button"
              :title="labelOf(technology)"
              class="relative flex h-21 shrink-0 cursor-pointer items-center gap-2.5 rounded-lg border border-accented bg-muted p-0.5 pe-2.5 text-start"
              :class="[
                technology.id === graph.root && 'border-primary bg-(--accent-soft) ring-1 ring-primary',
                lit && !isLit(technology) && technology.id !== graph.root && 'opacity-55',
              ]"
              @click="emit('ask', technology.id)"
            >
              <span
                v-if="technology.id === graph.root"
                class="absolute start-1 top-0 z-10 inline-flex h-4.5 items-center rounded-br-md bg-primary px-2 text-11 leading-none font-semibold text-(--accent-ink)"
              >
                Selected
              </span>
              <NodePicture :node="technology" class="h-full w-30 rounded-[5px] object-cover!" />
              <span class="flex min-w-0 flex-col gap-0.5">
                <b
                  class="truncate text-13 leading-4 font-medium text-highlighted"
                  :class="!technology.title && 'font-mono'"
                  >{{ labelOf(technology) }}</b
                >
                <small class="line-clamp-3 text-11 leading-3.5 text-muted">{{ technology.summary }}</small>
              </span>
            </button>
          </div>
        </div>
      </section>
    </div>
    <StatusFacts :facts="facts" />
  </div>
</template>
