<!-- A building opened on the canvas: its head, its table of levels, its production methods and what the picked ones come to. -->
<script setup lang="ts">
import { computed } from "vue";
import { createReusableTemplate } from "@vueuse/core";

import { Tone, type Amount, type Line, type Node, type Total } from "@services/atlas/models";
import { describeCount, toRoman } from "../../lib/format";
import OriginChip from "../../ui/OriginChip.vue";
import NodePicture, { labelOf } from "./NodePicture.vue";

const {
  node,
  line,
  selected = "",
} = defineProps<{
  node: Node;
  line: Line;
  /** The id of the canvas's selection, which marks its level's column. */
  selected?: string;
}>();
const emit = defineEmits<{
  /** A production method was clicked, to run in its group. */
  pick: [id: string];
  /** A level's column was clicked. */
  select: [id: string];
}>();

const TONES: Record<string, string> = { [Tone.ToneGain]: "text-(--green)", [Tone.ToneLoss]: "text-(--red)" };
const CELL = "h-7 border-t border-(--line-1) px-3 text-end whitespace-nowrap";
const BAND =
  "flex items-center gap-2 border-t border-(--line-1) bg-(--bg-1) px-3 pt-1.75 pb-1.25 text-11 text-(--fg-3)";
/** The columns of a production method's row: its name, what it takes, what it makes and whom it employs. */
const METHOD = "grid grid-cols-[13rem_minmax(0,1.1fr)_minmax(0,0.7fr)_minmax(0,1.2fr)] items-center gap-3 px-3";

const [DefineAmounts, Amounts] = createReusableTemplate<{ amounts: (Amount | Total)[] | null; made?: boolean }>();

const levels = computed(() => line.levels ?? []);
const groups = computed(() => line.groups ?? []);
/** A building of one level that has production methods says its costs in its head, and has no table. */
const hasTable = computed(() => levels.value.length > 1 || !groups.value.length);

/** describeUnlock returns the line under a method's name: whatever unlocks it, or that it needs nothing. */
function describeUnlock(unlocks: Node[] | null): string {
  return unlocks?.length ? unlocks.map(labelOf).join(", ") : "from the start";
}
</script>

<template>
  <DefineAmounts v-slot="{ amounts, made }">
    <span class="flex flex-wrap gap-x-2.5 gap-y-1">
      <span
        v-for="amount in amounts ?? []"
        :key="amount.good.id"
        :title="labelOf(amount.good)"
        class="inline-flex items-center gap-1 tabular-nums"
        :class="amount.text.startsWith('-') ? 'text-(--red)' : made ? 'font-semibold text-(--blue)' : 'text-(--fg-2)'"
      >
        <NodePicture :node="amount.good" class="size-4.5" />{{ amount.text }}
        <small v-if="'unscaled' in amount && amount.unscaled" class="text-[10.5px] font-normal text-(--fg-3)">
          for the building
        </small>
        <small v-if="'before' in amount && amount.before" class="text-[10.5px] font-normal text-(--fg-3)">
          ({{ amount.before }})
        </small>
      </span>
    </span>
  </DefineAmounts>

  <div
    class="relative flex min-h-18 items-center gap-3.5 border-b border-(--line-1) bg-[color-mix(in_srgb,var(--link)_7%,var(--bg-2))] py-2.5 ps-4 pe-7"
  >
    <NodePicture :node="node" class="size-12" />
    <span class="max-w-100 min-w-0 flex-1">
      <b class="block truncate text-16 leading-6 font-semibold text-(--fg-1)" :title="labelOf(node)">{{
        labelOf(node)
      }}</b>
      <small class="block truncate text-12 text-(--fg-3)" :title="node.summary">{{ node.summary }}</small>
    </span>
    <span v-if="!hasTable" class="flex shrink-0 gap-4.5 text-end">
      <span v-for="fact in node.facts ?? []" :key="fact.label" class="text-11 text-(--fg-3)">
        <b class="block text-13 leading-4.5 text-(--fg-1)">{{ fact.text }}</b>{{ fact.label }}
      </span>
    </span>
    <OriginChip :origin="node.origin" dot class="absolute end-2 top-2" />
  </div>

  <table v-if="hasTable && line.bands?.length" class="w-full border-collapse text-12 leading-4 tabular-nums">
    <thead>
      <tr>
        <th class="h-9.5 px-3 text-start font-semibold text-(--fg-2)">{{ levels.length > 1 ? "Level" : "" }}</th>
        <th
          v-for="level in levels"
          :key="level.level.id"
          class="h-9.5 cursor-pointer px-3 text-end align-middle font-semibold even:bg-(--fg-1)/3.5 hover:text-(--fg-1)"
          :class="level.level.id === selected ? 'text-primary shadow-[inset_0_-2px_0_var(--accent)]' : 'text-(--fg-2)'"
          :title="`Select ${labelOf(level.level)}`"
          @click.stop="emit('select', level.level.id)"
        >
          {{ levels.length > 1 ? toRoman(level.number) : "" }}
          <small
            v-if="levels.length > 1 && labelOf(level.level) !== labelOf(node)"
            class="block max-w-26 truncate text-[10.5px] leading-3.25 font-normal text-(--fg-3)"
          >
            {{ labelOf(level.level) }}
          </small>
          <small
            v-if="level.unlock"
            class="block max-w-26 truncate text-[10.5px] leading-3.25 font-normal text-(--fg-3)"
            :title="`Unlocked by ${labelOf(level.unlock)}`"
          >
            {{ labelOf(level.unlock) }}
          </small>
        </th>
      </tr>
    </thead>
    <tbody>
      <template v-for="band in line.bands" :key="band.title">
        <tr>
          <td
            :colspan="levels.length + 1"
            class="h-6 border-t border-(--line-1) bg-(--bg-1) px-3 text-11 text-(--fg-3)"
          >
            {{ band.title }}
          </td>
        </tr>
        <tr v-for="row in band.rows" :key="`${row.subject?.id}|${row.label}`">
          <td :class="CELL" class="max-w-60 truncate text-start font-medium text-(--fg-1)">
            <template v-if="row.subject">
              <NodePicture :node="row.subject" class="me-1.75 inline-block size-4.5 align-[-4px]" />{{
                labelOf(row.subject)
              }}
              <small class="ms-1 text-11 font-normal text-(--fg-3)">{{ row.label }}</small>
            </template>
            <template v-else>{{ row.label }}</template>
          </td>
          <td
            v-if="row.cells?.length === 1"
            :colspan="levels.length"
            :class="[CELL, TONES[row.tone ?? '']]"
            class="text-center"
          >
            {{ row.cells[0] }}
          </td>
          <template v-else>
            <td
              v-for="(cell, position) in row.cells"
              :key="position"
              :class="[CELL, TONES[row.tone ?? '']]"
              class="even:bg-(--fg-1)/3.5"
            >
              {{ cell }}
            </td>
          </template>
        </tr>
      </template>
    </tbody>
  </table>

  <template v-if="groups.length">
    <div :class="METHOD" class="min-h-7 bg-(--bg-3) text-11 font-semibold text-(--fg-2)">
      <span>Production method</span><span>Takes</span><span>Makes</span><span>Employs</span>
    </div>
    <template v-for="group in groups" :key="group.group.id">
      <div :class="BAND">
        <NodePicture v-if="group.group.id" :node="group.group" class="size-4" />
        <b class="font-semibold text-(--fg-2)">{{ group.group.id ? labelOf(group.group) : "Production methods" }}</b>
        one runs at a time
      </div>
      <button
        v-for="method in group.methods"
        :key="method.method.id"
        type="button"
        :class="[
          METHOD,
          method.method.id === group.picked && 'bg-(--bg-4) shadow-[inset_3px_0_0_var(--fg-2)]',
          method.late && 'opacity-45',
        ]"
        class="min-h-9 w-full cursor-pointer border-t border-(--line-1) py-1 text-start text-12 hover:bg-(--bg-3)"
        :aria-pressed="method.method.id === group.picked"
        :title="method.late ? 'Unlocked after the era you chose' : 'Run this method'"
        @click.stop="emit('pick', method.method.id)"
      >
        <span class="flex min-w-0 items-center gap-2">
          <NodePicture :node="method.method" class="size-6" />
          <span class="min-w-0">
            <b class="block truncate font-medium text-(--fg-1)" :title="labelOf(method.method)">{{
              labelOf(method.method)
            }}</b>
            <small class="block truncate text-[10.5px] leading-3.25 text-(--fg-3)">{{
              describeUnlock(method.unlocks)
            }}</small>
          </span>
        </span>
        <Amounts :amounts="method.takes" />
        <Amounts :amounts="method.makes" made />
        <Amounts :amounts="method.employs" />
      </button>
    </template>
  </template>

  <template v-if="line.totals">
    <div :class="BAND">
      <b class="font-semibold text-(--fg-2)">With these methods</b>
      {{ line.totals.jobs }}
    </div>
    <div :class="METHOD" class="min-h-9 border-t border-(--line-1) py-1.5 text-12">
      <span class="text-11 leading-3.5 text-(--fg-3)"
        >For one level at full employment, unless it says for the building</span
      >
      <Amounts :amounts="line.totals.takes" />
      <Amounts :amounts="line.totals.makes" made />
      <Amounts :amounts="line.totals.employs" />
    </div>
  </template>

  <p v-if="line.conditional" class="border-t border-(--line-1) px-3 py-2.25 text-12 text-(--fg-3)">
    <b class="font-medium text-(--fg-2)">{{ describeCount(line.conditional) }} more</b> only under a condition or a met
    need, listed in the detail pane.
  </p>
</template>
