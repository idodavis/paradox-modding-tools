<!--
  A building of one level as a sheet: its head with what it costs, its
  production methods where it names any, what it makes, needs and employs, and
  its bonuses under what they apply to.
-->
<script setup lang="ts">
import { computed } from "vue";
import type { Line, LineRow, Method } from "@services/graph/models";
import { toSentence } from "../../lib/format";
import { labelOf, TONES } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import SelectedTab from "./SelectedTab.vue";
import SubjectHead from "./SubjectHead.vue";
import UnlockLine from "./UnlockLine.vue";

const { line, selected = "" } = defineProps<{
  line: Line;
  /** The id of the selected production method or good, "" for none. */
  selected?: string;
}>();
const emit = defineEmits<{ select: [id: string]; ask: [id: string] }>();

/**
 * Block is the bonuses one condition holds for, and Section the blocks that
 * apply to one thing, as "In the province". A section's matrix is its blocks
 * with the same bonuses under different conditions, drawn as one table.
 */
type Block = { when: string; rows: LineRow[] };
type Section = { where: string; blocks: Block[]; matrix: Block[] };

/** MADE is the rows of what a building makes, with the word for each. */
const MADE: Record<string, string> = { output: "", boost: "boosts", gives: "gives" };
const CARD = "flex flex-col gap-3 rounded-[10px] border border-default bg-default p-4";
const HEADING = "flex items-baseline gap-2.5 text-14 font-semibold text-highlighted";
const PICKABLE =
  "relative cursor-pointer rounded-lg border border-accented bg-muted text-start hover:bg-elevated data-selected:border-primary data-selected:bg-(--accent-soft) data-selected:ring-1 data-selected:ring-primary";

const rows = computed(() => [...(line.rows ?? []), ...(line.same ?? [])]);
const level = computed(() => line.levels?.[0]);
const costs = computed(() =>
  rows.value.filter((row) => row.says === "cost").map((row) => ({ label: row.label, text: textOf(row) })),
);
const methods = computed(() =>
  (line.groups?.[0]?.methods ?? []).filter(
    (method) => method.makes?.length || method.takes?.length || method.gives?.length,
  ),
);
const makes = computed(() => (methods.value.length ? [] : rows.value.filter((row) => row.says in MADE && !row.need)));
const needs = computed(() => rows.value.filter((row) => row.says === "need"));
const employed = computed(() => rows.value.filter((row) => row.says === "employment"));

/**
 * sections are the bonuses no need gives, under what they apply to and then
 * under their condition.
 */
const sections = computed((): Section[] => {
  const bonuses = rows.value.filter((row) => row.says === "bonus" && !row.need);

  return [...Map.groupBy(bonuses, (row) => row.where ?? "")].map(([where, held]) => {
    const blocks = [...Map.groupBy(held, (row) => row.when ?? "")].map(([when, members]) => ({ when, rows: members }));
    const alike = Map.groupBy(
      blocks.filter((block) => block.when),
      (block) => block.rows.map((row) => row.label).join("|"),
    );
    const matrix = [...alike.values()].find((shared) => shared.length > 1) ?? [];

    return { where, matrix, blocks: blocks.filter((block) => !matrix.includes(block)) };
  });
});

function textOf(row: LineRow): string {
  return row.cells?.[0]?.text ?? "";
}

/** listGiven returns what meeting a need gives: the rows that name it. */
function listGiven(need: LineRow): LineRow[] {
  return rows.value.filter((row) => row.need === need.subject?.id);
}

function isOn(method: Method): boolean {
  return method.method.id === selected;
}
</script>

<template>
  <div class="flex h-full min-h-0 w-full min-w-0 flex-col gap-4 overflow-y-auto bg-(--bg-0) px-5 py-4">
    <SubjectHead :node="line.head" :group="line.group" :facts="costs">
      <UnlockLine :unlock="level?.unlock" :era="level?.era" :label="labelOf(line.head)" @ask="emit('ask', $event)" />
    </SubjectHead>

    <div class="grid shrink-0 grid-cols-[repeat(auto-fit,minmax(19rem,1fr))] items-start gap-4">
      <section v-if="methods.length" :class="CARD" class="col-span-2">
        <h2 :class="HEADING">
          Production methods
          <small v-if="methods.length > 1" class="text-12 font-normal text-dimmed">one runs at a time</small>
        </h2>
        <div class="grid grid-cols-2 gap-4">
          <button
            v-for="method in methods"
            :key="method.method.id"
            type="button"
            :data-selected="isOn(method) || undefined"
            :class="PICKABLE"
            class="flex flex-col gap-3 p-4"
            @click="emit('select', method.method.id)"
          >
            <SelectedTab v-if="isOn(method)" />
            <b class="text-14 text-highlighted">{{ labelOf(method.method) }}</b>
            <template
              v-for="part in [
                { title: 'Makes', amounts: method.makes },
                { title: 'Takes', amounts: method.takes },
              ]"
              :key="part.title"
            >
              <span v-if="part.amounts?.length" class="flex flex-col gap-1">
                <small class="text-11.5 text-dimmed">{{ part.title }}</small>
                <span
                  v-for="amount in part.amounts"
                  :key="amount.good.id"
                  class="flex h-8 items-center gap-3 text-13 text-highlighted"
                >
                  <NodeIcon :node="amount.good" class="size-7" />
                  {{ labelOf(amount.good) }}
                  <b class="grow text-end text-14 font-semibold text-(--blue) tabular-nums">{{ amount.text }}</b>
                </span>
              </span>
            </template>
            <span v-for="fact in method.gives" :key="fact.label" class="flex items-baseline gap-3 text-12 text-muted">
              {{ fact.label }}
              <b class="grow text-end font-semibold tabular-nums" :class="TONES[fact.tone ?? '']">{{ fact.text }}</b>
            </span>
            <span v-if="!method.makes?.length && !method.takes?.length" class="text-12 text-dimmed">
              Makes and takes no goods
            </span>
          </button>
        </div>
      </section>

      <section v-if="makes.length" :class="CARD">
        <h2 :class="HEADING">Makes</h2>
        <div
          v-for="row in makes"
          :key="`${row.says}|${row.subject?.id}|${row.when}`"
          class="flex items-center gap-3 text-14 text-highlighted"
        >
          <NodeIcon v-if="row.subject" :node="row.subject" class="size-8" />
          <span class="flex min-w-0 flex-col">
            {{ row.label }}
            <small v-if="MADE[row.says] || row.when" class="text-11.5 text-dimmed">
              {{ [MADE[row.says], row.when].filter(Boolean).join(", ") }}
            </small>
          </span>
          <b
            class="grow text-end text-18 font-semibold tabular-nums"
            :class="row.says === 'output' ? 'text-(--blue)' : 'text-(--purple)'"
          >
            {{ textOf(row) }}
          </b>
        </div>
      </section>

      <section v-if="needs.length" :class="CARD">
        <h2 :class="HEADING">
          Needs
          <small class="text-12 font-normal text-dimmed">meeting one gives what is under it</small>
        </h2>
        <button
          v-for="need in needs"
          :key="need.subject?.id"
          type="button"
          :data-selected="need.subject?.id === selected || undefined"
          :class="PICKABLE"
          class="flex flex-col gap-1.5 px-3.5 py-3"
          @click="need.subject && emit('select', need.subject.id)"
          @dblclick="need.subject && emit('ask', need.subject.id)"
        >
          <SelectedTab v-if="need.subject?.id === selected" />
          <span class="flex items-center gap-2.5 text-14 font-medium text-highlighted">
            <NodeIcon v-if="need.subject" :node="need.subject" class="size-7" />
            {{ need.label }}
            <b class="grow text-end text-16 font-semibold text-(--blue) tabular-nums">{{ textOf(need) }}</b>
          </span>
          <span
            v-for="bonus in listGiven(need)"
            :key="bonus.label"
            class="flex items-baseline gap-2 text-12 text-muted"
          >
            {{ bonus.label }}
            <small v-if="bonus.where" class="text-11.5 text-dimmed">{{ bonus.where.toLowerCase() }}</small>
            <b class="grow text-end font-semibold tabular-nums" :class="TONES[bonus.tone ?? ''] || 'text-(--purple)'">
              {{ textOf(bonus) }}
            </b>
          </span>
        </button>
      </section>

      <section v-if="employed.length" :class="CARD">
        <h2 :class="HEADING">Employs</h2>
        <div v-for="row in employed" :key="row.subject?.id" class="flex items-center gap-3 text-13 text-highlighted">
          <NodeIcon v-if="row.subject" :node="row.subject" class="size-7" />
          {{ row.label }}
          <b class="grow text-end text-14 font-semibold tabular-nums">{{ textOf(row) }}</b>
        </div>
      </section>

      <section v-for="section in sections" :key="section.where" :class="[CARD, section.matrix.length && 'col-span-2']">
        <h2 :class="HEADING">{{ section.where || "Bonuses" }}</h2>
        <div v-for="block in section.blocks" :key="block.when" class="flex flex-col">
          <small v-if="block.when" class="pb-1 text-11.5 text-dimmed">{{ toSentence(block.when) }}</small>
          <div
            v-for="row in block.rows"
            :key="row.label"
            class="flex min-h-8 items-baseline gap-3 border-t border-default py-1.5 text-12.5 text-highlighted first:border-t-0"
          >
            <span class="min-w-0 wrap-anywhere">{{ row.label }}</span>
            <b
              class="grow text-end text-13 font-semibold whitespace-nowrap tabular-nums"
              :class="TONES[row.tone ?? '']"
            >
              {{ textOf(row) }}
            </b>
          </div>
        </div>
        <table v-if="section.matrix.length" class="w-full text-12.5">
          <thead>
            <tr class="align-bottom text-11.5 text-dimmed">
              <th />
              <th v-for="block in section.matrix" :key="block.when" class="ps-3 pb-1.5 text-end font-normal">
                {{ toSentence(block.when) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, position) in section.matrix[0].rows" :key="row.label" class="border-t border-default">
              <td class="py-1.5 whitespace-nowrap text-highlighted">{{ row.label }}</td>
              <td
                v-for="block in section.matrix"
                :key="block.when"
                class="ps-3 text-end font-semibold tabular-nums"
                :class="TONES[block.rows[position].tone ?? '']"
              >
                {{ textOf(block.rows[position]) }}
              </td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </div>
</template>
