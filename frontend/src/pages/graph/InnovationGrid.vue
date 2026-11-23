<!--
  A game's innovations as a grid: a column for each era under its painting, a
  band for each group that opens to its cards, and chips that light the
  innovations that unlock one type of definition. No wires.
-->
<script setup lang="ts">
import { useQuery } from "@pinia/colada";
import { useFullscreen } from "@vueuse/core";
import { computed, shallowRef, useTemplateRef } from "vue";
import { Type } from "@internal/lang/models";
import type { Node, Technology, Tree, TreeGraph } from "@services/graph/models";
import { describeCount, plural, toSentence } from "../../lib/format";
import { graphCatalogsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import StatusFacts from "../../ui/StatusFacts.vue";
import { labelOf } from "./nodes";
import PaintingBand from "./PaintingBand.vue";
import TechCard from "./TechCard.vue";

const { tree } = defineProps<{ tree: TreeGraph }>();
const emit = defineEmits<{ ask: [id: string] }>();

const workspace = useWorkspaceStore();
const page = useTemplateRef("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);
const { data: catalogs } = useQuery(() => ({ ...graphCatalogsQuery, enabled: workspace.isOpen() }));

/** lit is the database of the chip that is on, "" for none. */
const lit = shallowRef("");
/** picked is the group the user opened or closed, unset until they do. */
const picked = shallowRef<string>();

const technologies = computed(() => tree.technologies ?? []);
const eras = computed(() => tree.eras ?? []);
const open = computed(() => picked.value ?? tree.tree);
const columns = computed(() => ({ gridTemplateColumns: `repeat(${eras.value.length}, minmax(0, 1fr))` }));
const noun = computed(() => technologies.value[0]?.node.what ?? "");
const title = computed(
  () => catalogs.value?.find((catalog) => catalog.types?.includes(Type.TypeTechnology))?.label ?? "",
);
const cells = computed(() => Map.groupBy(technologies.value, ({ node }) => toCell(node.group, node.era)));
const byEra = computed(() => Map.groupBy(technologies.value, ({ node }) => node.era ?? ""));
const litCount = computed(() => tree.unlocks?.find((unlocked) => unlocked.id === lit.value)?.count);
const facts = computed(() => [
  {
    label: toSentence(title.value),
    value: `${describeCount(technologies.value.length)} in ${plural(eras.value.length, "era")} and ${plural(tree.trees?.length ?? 0, "group")}`,
  },
]);

function toCell(group = "", era = ""): string {
  return `${group}|${era}`;
}

/**
 * describeEra says when an era starts and how many innovations it holds. A
 * game that writes no year says only how many.
 */
function describeEra(era: Node): string {
  const start = era.year === "0" ? "from the start" : era.year && `from ${era.year}`;

  return [start, plural(byEra.value.get(era.id)?.length ?? 0, noun.value)].filter(Boolean).join(" · ");
}

/**
 * countEras returns how many innovations a group holds in each era, as one
 * phrase where every era holds as many.
 */
function countEras(group: Tree): string[] {
  const counts = eras.value.map((era) => cells.value.get(toCell(group.id, era.id))?.length ?? 0);
  if (new Set(counts).size === 1) return [`${describeCount(counts[0] ?? 0)} in each era`];

  return eras.value.map((era, position) => `${labelOf(era)} ${describeCount(counts[position])}`);
}

function isLit(technology: Technology): boolean {
  return !!technology.unlocks?.some((unlocked) => unlocked.database === lit.value);
}

/** toggle opens a group, or closes the one that is open. */
function toggle(group: Tree): void {
  picked.value = open.value === group.id ? "" : group.id;
}
</script>

<template>
  <div ref="page" class="flex h-full min-h-0 w-full min-w-0 flex-col bg-(--bg-0)">
    <div class="flex h-11 shrink-0 items-center gap-2 border-b border-default bg-muted px-3 whitespace-nowrap">
      <h2 class="text-14 font-semibold text-highlighted">{{ toSentence(title) }}</h2>
      <span class="text-12 text-dimmed">by era and group</span>
      <template v-if="tree.unlocks?.length">
        <span class="ps-3 text-12 text-dimmed">Unlocks</span>
        <UButton
          v-for="unlocked in tree.unlocks"
          :key="unlocked.id"
          :label="toSentence(labelOf(unlocked))"
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
    </div>
    <div class="grid h-23 shrink-0 border-b border-default" :style="columns">
      <PaintingBand
        v-for="era in eras"
        :key="era.id"
        :painting="era.painting"
        class="flex flex-col justify-center gap-0.5 border-s border-default px-4 first:border-s-0"
      >
        <b class="text-14 text-highlighted">{{ labelOf(era) }}</b>
        <small class="text-11.5 text-muted">{{ describeEra(era) }}</small>
      </PaintingBand>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <section v-for="group in tree.trees" :key="group.id" class="border-b border-default">
        <PaintingBand :painting="group.painting" class="bg-muted">
          <button
            type="button"
            class="flex w-full cursor-pointer items-center gap-2.5 px-3.5 text-start text-12 whitespace-nowrap text-dimmed"
            :class="open === group.id ? 'h-13' : 'h-11'"
            :aria-expanded="open === group.id"
            @click="toggle(group)"
          >
            <UIcon
              :name="open === group.id ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              class="size-4 shrink-0"
            />
            <b class="text-13 font-semibold text-highlighted">{{ group.title }}</b>
            <span class="text-muted tabular-nums">{{ plural(group.count, noun) }}</span>
            <span v-for="count in countEras(group)" :key="count" class="ps-2 tabular-nums">{{ count }}</span>
          </button>
        </PaintingBand>
        <div v-if="open === group.id" class="grid border-t border-default bg-default/60" :style="columns">
          <div
            v-for="era in eras"
            :key="era.id"
            class="flex min-w-0 flex-col gap-1.25 border-s border-default p-2 first:border-s-0"
          >
            <TechCard
              v-for="technology in cells.get(toCell(group.id, era.id))"
              :key="technology.node.id"
              class="relative h-21 shrink-0"
              :class="lit && !isLit(technology) && technology.node.id !== tree.subject && 'opacity-55'"
              :technology="technology"
              shape="art"
              :mark="technology.node.id === tree.subject ? 'subject' : ''"
              @ask="emit('ask', $event)"
            />
          </div>
        </div>
      </section>
    </div>
    <StatusFacts :facts="facts" />
  </div>
</template>
