<!-- The card shown when the pointer rests on a word in the editor, with the word's facts and problems. -->
<script lang="ts">
/** A HoverProblem is one problem the editor marks on the word. */
export type HoverProblem = { id: string; level: string; message: string; line: number; column: number };
</script>

<script setup lang="ts">
import { computed } from "vue";
import { useLocalStorage } from "@vueuse/core";

import { ValueType } from "@internal/schema/models";
import type { Card } from "@services/lsp/models";

import { REASONS } from "../lib/format";
import ProblemRow from "./ProblemRow.vue";
import SourceLink from "./SourceLink.vue";

const { card, problems } = defineProps<{ card: Card | null; problems: HoverProblem[] }>();

const SITES_SHOWN = 4;
const KEYS = [
  ["F12", "definition"],
  ["Alt F12", "peek"],
  ["Shift F12", "references"],
];

// the scopes start closed and stay as the reader last left them
const scopesOpen = useLocalStorage("pmt.ide.scopes", false);

// what the word takes, such as its type and its choices
const takes = computed(() => {
  const expected = card?.takes;
  if (!expected) return "";
  const type = expected.type === ValueType.ValueUnknown ? "" : expected.type;
  return [
    type,
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
    <div v-if="problems.length" class="flex flex-col py-1">
      <ProblemRow
        v-for="(problem, index) in problems"
        :key="index"
        dense
        plain
        :level="problem.level"
        :message="problem.message"
        :detail="problem.id"
        mono
      >
        <template #trailing>
          <span class="shrink-0 font-mono text-11 text-dimmed">{{ problem.line }}:{{ problem.column }}</span>
        </template>
      </ProblemRow>
    </div>
    <template v-if="card">
      <div class="flex flex-col gap-1 px-3.5 py-2.5">
        <div class="flex min-w-0 items-center gap-2">
          <UBadge :label="card.class ? `${card.what} · ${card.class}` : card.what" color="info" size="sm" />
          <span class="min-w-0 font-mono text-13 font-semibold break-all text-highlighted">{{ card.title }}</span>
          <span class="grow" />
          <UBadge v-if="card.stale" label="outdated script_docs" color="warning" size="sm" />
        </div>
      </div>
      <div v-if="rows.length || card.color" class="px-3.5 py-2.5">
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
      <div v-if="card.blurb || card.about" class="flex flex-col gap-1.5 px-3.5 py-2.5">
        <p v-if="card.blurb" class="text-12 whitespace-pre-line text-toned">{{ card.blurb }}</p>
        <p v-if="card.about" class="text-11 text-dimmed">{{ card.about }}</p>
      </div>
      <!-- the scopes, tinted with the editor's scope color -->
      <div v-if="card.scopes?.length" class="px-3 py-2">
        <UCollapsible
          v-model:open="scopesOpen"
          class="rounded-md border border-(--syn-scope)/35 bg-(--syn-scope)/8 text-11"
        >
          <button type="button" class="flex w-full items-center gap-2 px-2.5 py-1.5 text-toned hover:text-highlighted">
            <UIcon
              name="i-lucide-chevron-right"
              class="size-3.5 text-(--syn-scope) transition-transform"
              :class="scopesOpen && 'rotate-90'"
            />
            <span class="font-semibold">Scopes</span>
            <span class="font-mono text-dimmed">{{ card.scopes[0]!.link }} {{ card.scopes[0]!.type }}</span>
            <span class="ms-auto text-dimmed">{{ card.scopes.length }}</span>
          </button>
          <template #content>
            <div
              class="grid grid-cols-[auto_auto_minmax(0,1fr)] items-baseline gap-x-2.5 gap-y-1 border-t border-(--syn-scope)/20 px-2.5 py-2 ps-8"
            >
              <template v-for="scope in card.scopes" :key="scope.link">
                <span class="font-mono text-toned">{{ scope.link }}</span>
                <span class="font-mono text-highlighted">{{ scope.type }}</span>
                <span class="text-dimmed">{{ scope.about }}</span>
              </template>
            </div>
          </template>
        </UCollapsible>
      </div>
      <div v-if="sites.length" class="flex flex-col gap-1.5 px-3.5 py-2.5">
        <div v-for="site in sites" :key="`${site.place.key}:${site.line}`" class="flex min-w-0 items-center gap-2">
          <SourceLink :place="site.place" :line="site.line" />
          <UBadge v-if="site.kept" label="Kept" color="success" size="sm" />
          <UBadge v-if="site.reason" :label="REASONS[site.reason]?.label ?? site.reason" size="sm" />
        </div>
        <span v-if="(card.sites?.length ?? 0) > SITES_SHOWN" class="text-11 text-dimmed"
          >+{{ (card.sites?.length ?? 0) - SITES_SHOWN }} more</span
        >
      </div>
      <p v-if="card.inferred" class="px-3.5 py-1.5 text-end text-tiny text-dimmed">inferred from game files</p>
      <div
        class="flex flex-wrap items-center gap-x-3 gap-y-1 bg-muted px-3.5 py-1.5 text-tiny whitespace-nowrap text-dimmed"
      >
        <span v-for="[key, action] in KEYS" :key="key" class="flex items-center gap-1"
          ><UKbd :value="key" />{{ action }}</span
        >
        <span v-if="card.node" class="flex items-center gap-1"><UKbd value="Ctrl Shift G" />graph</span>
        <span class="ms-auto">Right-click for more</span>
      </div>
    </template>
  </div>
</template>
