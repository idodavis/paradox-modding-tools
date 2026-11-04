<!--
  The card shown when the pointer rests on a word in the editor, with the word's facts and problems. The simple
  interface keeps it to the title, the blurb, the problems and where the word is defined.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { ValueType } from "@internal/schema/models";
import type { Card, Diagnostic } from "@services/lsp/models";

import { codesQuery, levelsQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import ProblemRow from "./ProblemRow.vue";
import SourceLink from "./SourceLink.vue";

const { card, problems } = defineProps<{ card: Card | null; problems: Diagnostic[] }>();

const SITES_SHOWN = 4;
const KEYS = [
  ["F12", "definition"],
  ["Alt F12", "peek"],
  ["Shift F12", "references"],
];

const settings = useSettingsStore();
const { data: codes } = useQuery(codesQuery);
const { data: levels } = useQuery(levelsQuery);
const short = computed(() => settings.isHidden("hover.details"));

/** describeProblem returns the label and text of a problem's cost, from its level, and of its advice, from its code. */
function describeProblem(problem: Diagnostic): [string, string][] {
  const all = [
    ["What it costs", levels.value?.find((level) => level.id === problem.level)?.cost],
    ["What to do", codes.value?.find((code) => code.id === problem.code)?.advice],
  ];
  return all.filter((row): row is [string, string] => !!row[1]);
}

// what the word takes, such as its type and its choices
const takes = computed(() => {
  const expected = card?.takes;
  if (!expected) return "";
  const type = expected.type === ValueType.ValueUnknown ? "" : expected.type;
  return [
    type,
    expected.scope && `leading to ${expected.scope}`,
    expected.database && `of ${expected.database}`,
    expected.choices?.join(" | "),
    expected.values && "or a script value",
  ]
    .filter(Boolean)
    .join(" ");
});
// the label and value of each fact the card holds
const rows = computed(() => {
  const stamp = card?.date;
  const header = card?.image;
  const size = header && `${header.width}×${header.height} ${header.format}`;
  const mips = header && header.mipmaps > 1 && `${header.mipmaps} mip levels`;
  const all: [string, string | null | undefined][] = [
    ["Needed", card?.need],
    ["Works in", card?.runs],
    ["Takes", takes.value || card?.target],
    ["Usually", card?.writes?.join(", ")],
    ["Returns", card?.returns],
    ["Switches to", card?.leads],
    ["Its block", card?.holds],
    ["Date", stamp && [stamp.date.day, stamp.month, stamp.date.year, stamp.era].filter(Boolean).join(" ")],
    ["Number", card?.number?.toString()],
    ["Weight", card?.weight ? `${card.weight}% of its siblings` : undefined],
    ["Image", header && [size, header.compression, mips].filter(Boolean).join(", ")],
  ];
  return all.filter((row): row is [string, string] => !!row[1]);
});
const sites = computed(() => (card?.sites ?? []).slice(0, SITES_SHOWN));
</script>

<template>
  <div class="flex w-max max-w-198 flex-col divide-y divide-default overflow-hidden">
    <!-- the problems are set apart from the facts below them by a line that shows on the card's background -->
    <div v-if="problems.length" class="flex flex-col py-1" :class="card && 'border-b border-accented'">
      <div v-for="(problem, index) in problems" :key="index" class="flex flex-col">
        <ProblemRow dense plain :level="problem.level" :message="problem.message" :detail="problem.code" mono>
          <template #trailing>
            <span class="shrink-0 font-mono text-11 text-dimmed"
              >{{ problem.range.startLineNumber }}:{{ problem.range.startColumn }}</span
            >
          </template>
        </ProblemRow>
        <div class="grid grid-cols-[78px_minmax(0,1fr)] gap-x-2.5 gap-y-1 px-3 pt-1 pb-1.5 text-12">
          <template v-for="[label, text] in describeProblem(problem)" :key="label">
            <span class="text-dimmed">{{ label }}</span>
            <span class="text-toned">{{ text }}</span>
          </template>
        </div>
      </div>
    </div>
    <template v-if="card">
      <div class="flex flex-col gap-1 px-3.5 py-2.5">
        <div class="flex min-w-0 items-center gap-2">
          <UBadge :label="card.class ? `${card.what} · ${card.class}` : card.what" color="info" size="sm" />
          <span class="min-w-0 font-mono text-13 font-semibold break-all text-highlighted">{{ card.title }}</span>
          <span class="grow" />
          <UTooltip
            v-if="card.stale"
            text="The game's documentation is older than the game. Type script_docs, then dump_data_types, in the game's console to write it again."
          >
            <UBadge label="Outdated: rerun script_docs" color="warning" size="sm" />
          </UTooltip>
        </div>
      </div>
      <div v-if="!short && (rows.length || card.color)" class="px-3.5 py-2.5">
        <div class="grid grid-cols-[78px_minmax(0,1fr)] items-baseline gap-x-2.5 gap-y-1 text-12">
          <template v-for="[label, value] in rows" :key="label">
            <span class="text-dimmed">{{ label }}</span>
            <span class="line-clamp-3 font-mono text-11 break-words text-toned" :title="value">{{ value }}</span>
          </template>
          <template v-if="card.color">
            <span class="text-dimmed">Color</span>
            <span class="flex items-center gap-2 font-mono text-11 text-toned">
              <span class="size-3 rounded-sm border border-default" :style="{ background: card.color }" />
              {{ card.color }}
            </span>
          </template>
        </div>
      </div>
      <div v-if="card.quote || card.blurb || (!short && card.about)" class="flex flex-col gap-1.5 px-3.5 py-2.5">
        <!-- the game's own text, set apart from what PMT says of the word -->
        <blockquote v-if="card.quote" class="border-l-2 border-accented ps-2.5 text-12 whitespace-pre-line text-toned">
          {{ card.quote }}
        </blockquote>
        <p v-if="card.blurb" class="text-12 whitespace-pre-line text-toned">{{ card.blurb }}</p>
        <p v-if="!short && card.about" class="text-11 text-dimmed">{{ card.about }}</p>
      </div>
      <div v-if="sites.length" class="flex flex-col gap-1.5 px-3.5 py-2.5">
        <div v-for="site in sites" :key="`${site.place.key}:${site.line}`" class="flex min-w-0 items-center gap-2">
          <SourceLink :place="site.place" :line="site.line" />
          <UBadge v-if="site.holder" :label="site.holder" size="sm" class="font-mono" />
          <!-- the holder stands for a kept copy's badges; a skipped copy still says why -->
          <template v-if="!site.holder || !site.kept">
            <UBadge v-if="site.kept && (card.sites?.length ?? 0) > 1" label="Kept" color="success" size="sm" />
            <UBadge v-if="site.label" :label="site.label" size="sm" />
          </template>
        </div>
        <span v-if="(card.sites?.length ?? 0) > SITES_SHOWN" class="text-11 text-dimmed"
          >+{{ (card.sites?.length ?? 0) - SITES_SHOWN }} more</span
        >
      </div>
      <p v-if="!short && card.inferred" class="px-3.5 py-1.5 text-end text-tiny text-dimmed">
        inferred from game files
      </p>
      <div
        v-if="!short"
        class="flex flex-wrap items-center gap-x-3 gap-y-1 bg-muted px-3.5 py-1.5 text-tiny whitespace-nowrap text-dimmed"
      >
        <span v-for="[key, action] in KEYS" :key="key" class="flex items-center gap-1">
          <UKbd :value="key" />{{ action }}
        </span>
        <span v-if="card.node" class="flex items-center gap-1"> <UKbd value="Ctrl Shift G" />graph </span>
        <span class="ms-auto">Right-click for more</span>
      </div>
    </template>
  </div>
</template>
