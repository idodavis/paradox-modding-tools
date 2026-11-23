<!--
  Rows of a building's line as one compact table: a column for each level and
  a row for each fact, under what it says and the condition it holds for.
-->
<script setup lang="ts">
import { computed } from "vue";
import { Among, type Line, type LineRow, type Node } from "@services/graph/models";
import { toRoman, toSentence } from "../../lib/format";
import { conditionOf, keyOf, labelOf, SAYS, TONES } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import SelectedTab from "./SelectedTab.vue";
import UnlockLine from "./UnlockLine.vue";

const { line, rows, selected } = defineProps<{
  line: Line;
  /** The rows listed. One with a single cell says it for every level. */
  rows: LineRow[];
  /** The id of the selected level. */
  selected: string;
}>();
const emit = defineEmits<{ select: [id: string]; ask: [id: string] }>();

/**
 * GRID is the columns of the table: the labels, as wide as the longest up to
 * a limit, then each level at a width that holds "12 months".
 */
const GRID = "grid grid-cols-[fit-content(20rem)_repeat(var(--levels),minmax(4.75rem,6.5rem))]";
const CELL = "flex h-7.5 items-center border-t border-default";

const levels = computed(() => line.levels ?? []);
const every = computed(() => [...(line.rows ?? []), ...(line.same ?? [])]);
const chosen = computed(() => levels.value.findIndex((level) => level.level.id === selected));
const hasArt = computed(() => new Set(levels.value.map((level) => level.level.picture)).size === levels.value.length);
const hasUnlocks = computed(() => levels.value.some((level) => level.unlock));
/** stretches are the runs of levels one technology unlocks. */
const stretches = computed(() => {
  const runs: { unlock?: Node | null; era?: Node | null; count: number; label: string }[] = [];
  for (const { level, unlock, era } of levels.value) {
    const last = runs.at(-1);
    if (last && last.unlock?.id === unlock?.id) last.count++;
    else runs.push({ unlock, era, count: 1, label: labelOf(level) });
  }

  return runs;
});

/**
 * sections are the rows by what they say and then by the condition they
 * share, so a condition is said once.
 */
const sections = computed(() =>
  Object.entries(SAYS)
    .map(([says, heading]) => ({
      says,
      ...heading,
      blocks: [...Map.groupBy(listRows(says), (row) => conditionOf(every.value, row))],
    }))
    .filter((section) => section.blocks.length),
);

function listRows(says: string): LineRow[] {
  return rows.filter((row) => row.says === says);
}

/**
 * hasOr reports whether a row is a second way of working: a good a level
 * takes in place of an earlier row's.
 */
function hasOr(row: LineRow): boolean {
  const alike = listRows(row.says).filter((other) => other.among === Among.AmongEither);

  return alike.indexOf(row) > 0;
}

/** isWide reports whether a row says one thing for every level. */
function isWide(row: LineRow): boolean {
  return (row.cells?.length ?? 0) < levels.value.length;
}

function select(position: number): void {
  emit("select", levels.value[position].level.id);
}

function ask(position: number): void {
  emit("ask", levels.value[position].level.id);
}
</script>

<template>
  <div
    class="w-fit max-w-full shrink-0 overflow-x-auto rounded-xl border border-default bg-muted"
    :style="{ '--levels': levels.length }"
  >
    <div :class="GRID" class="relative">
      <template v-if="hasUnlocks">
        <span class="self-center px-3.5 pt-2 text-12 text-dimmed">Unlocked by</span>
        <span
          v-for="(stretch, position) in stretches"
          :key="position"
          :style="{ gridColumn: `span ${stretch.count}` }"
          class="mx-1 mt-2 flex min-h-6 items-center justify-center rounded-md border bg-elevated px-2 py-0.5"
          :class="stretch.unlock ? 'border-default' : 'border-dashed border-default'"
        >
          <UnlockLine :unlock="stretch.unlock" :era="stretch.era" :label="stretch.label" @ask="emit('ask', $event)" />
        </span>
      </template>
      <span class="self-center px-3.5 text-12 text-dimmed">Level</span>
      <button
        v-for="(level, position) in levels"
        :key="level.level.id"
        type="button"
        :aria-pressed="position === chosen"
        :title="labelOf(level.level)"
        class="flex cursor-pointer flex-col items-center gap-0.5 px-1 py-2 text-12 font-semibold text-muted hover:text-highlighted aria-pressed:text-highlighted"
        @click="select(position)"
        @dblclick="ask(position)"
      >
        <NodeIcon v-if="hasArt" :node="level.level" class="size-7" />
        <template v-else>{{ toRoman(level.number) }}</template>
        <small v-if="hasArt" class="line-clamp-2 text-center text-11 leading-3.5 font-normal">
          {{ labelOf(level.level) }}
        </small>
      </button>

      <template v-for="section in sections" :key="section.says">
        <h2 class="col-span-full border-t border-default bg-elevated px-3.5 py-1 text-12 font-semibold text-muted">
          {{ section.title }}
        </h2>
        <template v-for="[condition, held] in section.blocks" :key="condition">
          <template v-if="condition">
            <small class="border-t border-default px-3.5 pt-1.5 text-11.5 text-dimmed">{{
              toSentence(condition)
            }}</small>
            <span class="col-[2/-1] border-s border-t border-default" />
          </template>
          <template v-for="row in held" :key="keyOf(row)">
            <span
              :class="[CELL, condition && 'border-t-0']"
              class="min-w-0 gap-1.5 px-3.5 text-12 font-medium text-highlighted"
              :title="row.label"
              @dblclick="row.subject && emit('ask', row.subject.id)"
            >
              <small v-if="hasOr(row)" class="text-11.5 font-normal text-dimmed">or</small>
              <NodeIcon v-if="row.subject" :node="row.subject" class="size-5" />
              <span class="truncate">{{ row.label }}</span>
            </span>
            <span
              v-if="isWide(row)"
              :class="[CELL, condition && 'border-t-0', section.tone || TONES[row.tone ?? '']]"
              class="col-[2/-1] justify-center border-s px-1 text-12.5 whitespace-nowrap tabular-nums"
            >
              {{ row.cells?.[0]?.text }}
            </span>
            <template v-else>
              <span
                v-for="(cell, position) in row.cells"
                :key="position"
                :class="[CELL, condition && 'border-t-0', section.tone || TONES[row.tone ?? '']]"
                class="relative cursor-pointer justify-center border-s px-1 text-12.5 whitespace-nowrap tabular-nums"
                @click="select(position)"
                @dblclick="ask(position)"
              >
                {{ cell.text }}
                <i
                  v-if="row.says === 'output' && cell.part"
                  class="absolute start-2 bottom-0.5 h-0.75 rounded-full bg-(--blue)"
                  :class="position !== chosen && 'opacity-50'"
                  :style="{ width: `calc(${cell.part} * (100% - 1rem))` }"
                />
              </span>
            </template>
          </template>
        </template>
      </template>
      <div
        v-if="chosen >= 0"
        class="pointer-events-none absolute inset-0 rounded-md border-2 border-primary"
        :style="{ gridColumn: `${chosen + 2} / ${chosen + 3}` }"
      >
        <SelectedTab />
      </div>
    </div>
  </div>
</template>
