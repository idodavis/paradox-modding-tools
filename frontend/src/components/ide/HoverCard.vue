<script setup lang="ts">
/**
 * What a word is (the Ide board's hover card, O-R7, O-R51): the problems found there, its kind, name and scope, the
 * facts it carries (what it takes and returns, ...), its text and prose, and last where it is defined and what it
 * replaces. Where the backend has no card (plain text in a localization value) the problems show alone.
 */
import { computed } from "vue";
import type { editor } from "monaco-editor/editor/editor.api";
import type { Inspect, Location } from "@services/internal/lsp";
import { useIdeStore } from "../../stores/ide";
import { useWorkspaceStore } from "../../stores/workspace";

const { card, problems = [] } = defineProps<{ card: Inspect | null; problems?: editor.IMarker[] }>();

const store = useWorkspaceStore();
const ide = useIdeStore();

const sites = computed(() => card?.sites ?? []);
const winner = computed(() => card?.winner ?? -1);
const kept = computed(() => sites.value[winner.value]);
// each other site by its part: added to the kept one, replaced by it, or one of several where none is kept
const others = computed(() =>
  sites.value.flatMap((site, i) =>
    i === winner.value ? [] : [{ site, part: winner.value < 0 ? "Defined by" : card?.adds?.includes(i) ? "Adds to it" : "Replaces" }],
  ),
);
const overrides = computed(
  () =>
    !!kept.value && kept.value.origin !== store.originVanilla &&
    others.value.some(({ site, part }) => part === "Replaces" && site.origin === store.originVanilla),
);
const facts = computed(() => {
  if (!card) return [];
  return [
    ["Needed", card.need],
    ["Takes", (card.takes?.length ? card.takes : card.writes)?.join("; ")],
    ["Returns", card.returns],
    ["Leads to", card.leads],
    ["Date", card.date],
    ["Weight", card.weight ? `${card.weight}% of its siblings` : ""],
    ["Image", card.image && `${card.image.width}×${card.image.height} ${card.image.format}${card.image.mips ? `, ${card.image.mips} mips` : ""}`],
  ].filter((fact): fact is [string, string] => !!fact[1]);
});
const color = computed(() => (/^#[0-9a-f]{6}$/i.test(card?.color ?? "") ? card?.color : ""));
// a word that says nothing of itself shows only its problems
const said = computed(() => !!(card?.head || card?.word || card?.key));
const KEYS = [
  ["F12", "definition"],
  ["Alt F12", "peek"],
  ["Shift F12", "references"],
];

function open(site: Location): void {
  ide.open(site.path, { line: site.range.start.line, col: site.range.start.character });
}
</script>

<template>
  <div class="flex w-132 flex-col overflow-hidden divide-y divide-default">
    <div v-if="problems.length" class="flex flex-col gap-1 px-3.5 py-2.5">
      <div v-for="(problem, i) in problems" :key="i" class="flex items-start gap-2 text-12">
        <UIcon
          :name="problem.severity === 8 ? 'i-lucide-circle-x' : 'i-lucide-triangle-alert'"
          class="mt-0.5 size-3.5 shrink-0"
          :class="problem.severity === 8 ? 'text-error' : 'text-warning'"
        />
        <span class="text-toned">{{ problem.message }}</span>
        <span class="grow" />
        <span class="shrink-0 font-mono text-11 text-dimmed">{{ typeof problem.code === "string" ? problem.code : problem.code?.value }}</span>
      </div>
    </div>
    <template v-if="card">
      <div v-if="said" class="flex flex-col gap-1 px-3.5 py-2.5">
        <div class="flex min-w-0 items-center gap-2">
          <UBadge v-if="card.head" :label="card.head" color="info" size="sm" />
          <span class="truncate font-mono text-13 font-semibold text-highlighted">{{ card.word || card.key }}</span>
          <span class="grow" />
          <UBadge v-if="card.stale" label="outdated script_docs" color="warning" size="sm" />
          <UBadge v-if="overrides" label="Overrides the game" color="purple" size="sm" />
        </div>
        <div v-if="card.scope" class="font-mono text-11 text-dimmed">{{ card.scope }} scope</div>
      </div>
      <div v-if="facts.length || color" class="px-3.5 py-2.5">
        <div class="grid grid-cols-[78px_minmax(0,1fr)] items-center gap-x-2.5 gap-y-1 text-12">
          <template v-for="[label, value] in facts" :key="label">
            <span class="text-dimmed">{{ label }}</span>
            <span class="flex min-w-0 items-center gap-2 font-mono text-11 text-toned">
              <span class="truncate">{{ value }}</span>
              <UBadge v-if="label === 'Takes' && !card.takes?.length" label="inferred from vanilla" size="sm" />
            </span>
          </template>
          <template v-if="color">
            <span class="text-dimmed">Color</span>
            <span class="flex items-center gap-2 font-mono text-11 text-toned">
              <span class="size-3 rounded-sm border border-default" :style="{ background: color }" />{{ color }}
            </span>
          </template>
        </div>
      </div>
      <div v-if="card.name || card.prose || card.about" class="flex flex-col gap-1.5 px-3.5 py-2.5">
        <LocQuote v-if="card.name" :text="card.name" />
        <p v-if="card.prose" class="whitespace-pre-line text-12 text-toned">{{ card.prose }}</p>
        <p v-if="card.about" class="text-11 text-dimmed">{{ card.about }}</p>
      </div>
      <div v-if="sites.length" class="px-3.5 py-2.5">
        <div class="grid grid-cols-[78px_minmax(0,1fr)] items-center gap-x-2.5 gap-y-1 text-12">
          <template v-for="({ site, part }, i) in kept ? [{ site: kept, part: 'Defined by' }, ...others] : others" :key="i">
            <span class="text-dimmed">{{ part }}</span>
            <span class="flex min-w-0 items-center gap-2">
              <OriginChip :label="store.originName(site.origin)" :color="store.originColor(site.origin)" small />
              <UButton
                :label="`${site.rel}:${site.range.start.line + 1}`"
                variant="link"
                color="neutral"
                size="xs"
                class="min-w-0 p-0 font-mono text-11"
                :ui="{ label: 'truncate' }"
                @click="open(site)"
              />
            </span>
          </template>
        </div>
      </div>
      <!-- each key with its word on one line; a narrow card wraps between them -->
      <div v-if="said" class="flex flex-wrap items-center gap-x-3 gap-y-1 bg-muted px-3.5 py-1.5 text-tiny whitespace-nowrap text-dimmed">
        <span v-for="[key, word] in KEYS" :key="key" class="flex items-center gap-1"><UKbd :value="key" />{{ word }}</span>
        <span v-if="card.graphed" class="flex items-center gap-1"><UKbd value="Ctrl Shift G" />graph</span>
        <span class="ms-auto">Right-click for more</span>
      </div>
    </template>
  </div>
</template>
