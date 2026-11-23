<!--
  A building line of several levels: its head, the table of what the levels
  say differently, and beside it what is the same on every level.
-->
<script setup lang="ts">
import { computed } from "vue";
import { uniqBy } from "es-toolkit";
import type { Line } from "@services/graph/models";
import { list, plural, toSentence } from "../../lib/format";
import LineTable from "./LineTable.vue";
import { conditionOf, keyOf, labelOf, SAYS, TONES } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import SubjectHead from "./SubjectHead.vue";

const { line, selected } = defineProps<{
  line: Line;
  /** The id of the selected level. */
  selected: string;
}>();
const emit = defineEmits<{ select: [id: string]; ask: [id: string] }>();

const rows = computed(() => [...(line.rows ?? []), ...(line.same ?? [])]);
const makes = computed(() =>
  uniqBy(
    rows.value.flatMap((row) => (row.says === "output" && row.subject ? [row.subject] : [])),
    (good) => good.id,
  ),
);
const sameBlocks = computed(() => [...Map.groupBy(line.same ?? [], (row) => conditionOf(rows.value, row))]);
</script>

<template>
  <div class="flex min-h-0 flex-col gap-3 overflow-auto bg-default p-4">
    <SubjectHead :node="line.head" :group="line.group">
      <span>{{ plural(line.levels?.length ?? 0, "level") }}</span>
      <span v-if="makes.length" class="flex items-center gap-1.5">
        Makes
        <NodeIcon v-for="good in makes" :key="good.id" :node="good" class="size-5" />
        <b class="font-medium text-highlighted">{{ list(makes.map(labelOf)) }}</b>
      </span>
    </SubjectHead>

    <div class="flex flex-wrap items-start gap-4 pt-4.5">
      <LineTable
        :line="line"
        :rows="line.rows ?? []"
        :selected="selected"
        @select="emit('select', $event)"
        @ask="emit('ask', $event)"
      />

      <section v-if="sameBlocks.length" class="flex w-80 shrink-0 flex-col gap-2">
        <h2 class="text-13 font-semibold text-highlighted">The same on every level</h2>
        <div
          v-for="[condition, held] in sameBlocks"
          :key="condition"
          class="flex flex-col rounded-lg border border-default bg-muted px-3.5 py-2"
        >
          <small v-if="condition" class="pb-1 text-11.5 text-dimmed">{{ toSentence(condition) }}</small>
          <div
            v-for="row in held"
            :key="keyOf(row)"
            class="flex min-h-7 items-center gap-2 border-t border-default py-1 text-12.5 text-highlighted first:border-t-0"
            @dblclick="row.subject && emit('ask', row.subject.id)"
          >
            <NodeIcon v-if="row.subject" :node="row.subject" class="size-5" />
            <span class="min-w-0 wrap-anywhere">{{ row.label }}</span>
            <small v-if="row.subject" class="text-11.5 text-dimmed">{{ SAYS[row.says]?.title.toLowerCase() }}</small>
            <b
              class="grow text-end font-semibold whitespace-nowrap tabular-nums"
              :class="SAYS[row.says]?.tone || TONES[row.tone ?? '']"
            >
              {{ row.cells?.[0]?.text }}
            </b>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
