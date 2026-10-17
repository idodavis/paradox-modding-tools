<script setup lang="ts">
/**
 * What a word is: a lead the caller draws above it, the problems found there, its kind, name and scope, the
 * facts it carries (what it takes and returns, ...), its text and prose, and last where it is defined and what it
 * replaces. Where the backend has no card (plain text in a localization value) the problems show alone.
 */
import { computed } from "vue";
import type { monaco } from "../lib/monaco";
import type { Inspect, Location } from "@services/internal/lsp";
import { useEditorStore } from "../stores/editor";
import { useWorkspaceStore } from "../stores/workspace";
import LocQuote from "./LocQuote.vue";
import OriginChip from "./OriginChip.vue";

const {
  card,
  problems = [],
  size = "full",
  category = "",
} = defineProps<{
  card: Inspect | null;
  problems?: monaco.editor.IMarker[];
  size?: "full" | "compact";
  // the category the code under the card is colored in, which its head's badge takes
  category?: string;
}>();

const store = useWorkspaceStore();
const editor = useEditorStore();

const sites = computed(() => card?.sites ?? []);
const winner = computed(() => card?.winner ?? -1);
const winningSite = computed(() => sites.value[winner.value]);
// each other site by its part: added to the kept one, replaced by it, or one of several where none is kept
const others = computed(() =>
  sites.value.flatMap((site, i) =>
    i === winner.value
      ? []
      : [{ site, part: winner.value < 0 ? "Defined by" : card?.adds?.includes(i) ? "Adds to it" : "Replaces" }],
  ),
);
// each size's width, its sections' padding, how many files an origin lists before it counts the rest (so a name
// written in many files keeps the card short), and whether the editor's keys end it
const SIZES = {
  full: { root: "max-w-198", section: "px-3.5 py-2.5", shown: 2, keys: true },
  compact: { root: "max-w-90 text-12", section: "px-2.5 py-1.5", shown: 1, keys: false },
};
const look = computed(() => SIZES[size]);
/** The sites as rows: a part's sites of one origin together, each file once with how many places it holds. */
const groups = computed(() => {
  const rows: { part: string; origin: string; files: { rel: string; first: Location; count: number }[] }[] = [];
  for (const { site, part } of winningSite.value
    ? [{ site: winningSite.value, part: "Defined by" }, ...others.value]
    : others.value) {
    let group = rows.find((row) => row.part === part && row.origin === site.origin);
    if (!group) rows.push((group = { part, origin: site.origin, files: [] }));
    const file = group.files.find((one) => one.rel === site.rel);
    if (file) file.count++;
    else group.files.push({ rel: site.rel, first: site, count: 1 });
  }
  return rows;
});
const overrides = computed(
  () =>
    !!winningSite.value &&
    winningSite.value.origin !== store.originVanilla &&
    others.value.some(({ site, part }) => part === "Replaces" && site.origin === store.originVanilla),
);
const facts = computed(() => {
  if (!card) return [];
  return [
    ["Needed", card.need],
    ["Takes", (card.takes?.length ? card.takes : card.writes)?.join("; ")],
    ["Returns", card.returns],
    ["Leads to", card.leads],
    ["Root", card.scope?.root !== card.scope?.this ? card.scope?.root : ""],
    [
      "Previous",
      [...(card.scope?.prev ?? [])]
        .reverse()
        .map((scope) => scope || "unknown")
        .join(", "),
    ],
    [
      "Saved",
      Object.entries(card.scope?.named ?? {})
        .map(([name, type]) => `scope:${name} = ${type || "unknown"}`)
        .join("; "),
    ],
    ["Date", card.date],
    ["Weight", card.weight ? `${card.weight}% of its siblings` : ""],
    [
      "Image",
      card.image &&
        `${card.image.width}×${card.image.height} ${card.image.format}${card.image.mips ? `, ${card.image.mips} mips` : ""}`,
    ],
  ].filter((fact): fact is [string, string] => !!fact[1]);
});
const color = computed(() => (/^#[0-9a-f]{6}$/i.test(card?.color ?? "") ? card?.color : ""));
// the head's badge in the code's color (its category's --syn- token), soft as the theme's soft fills; else the info color
const headStyle = computed(
  () =>
    category && {
      color: `var(--syn-${category})`,
      background: `color-mix(in srgb, var(--syn-${category}) 16%, transparent)`,
    },
);
// a word that says nothing of itself shows only its problems
const identified = computed(() => !!(card?.head || card?.word || card?.key));
const KEYS = [
  ["F12", "definition"],
  ["Alt F12", "peek"],
  ["Shift F12", "references"],
];

function open(site: Location): void {
  editor.open(site.path, { line: site.range.start.line, col: site.range.start.character });
}
</script>

<template>
  <div class="flex w-max flex-col overflow-hidden divide-y divide-default" :class="look.root">
    <!-- as wide as its content, up to its size's width -->
    <div v-if="$slots.lead" :class="look.section"><slot name="lead" /></div>
    <div v-if="problems.length" class="flex flex-col gap-1" :class="look.section">
      <div v-for="(problem, i) in problems" :key="i" class="flex items-start gap-2 text-12">
        <UIcon
          :name="problem.severity === 8 ? 'i-lucide-circle-x' : 'i-lucide-triangle-alert'"
          class="mt-0.5 size-3.5 shrink-0"
          :class="problem.severity === 8 ? 'text-error' : 'text-warning'"
        />
        <span class="text-toned">{{ problem.message }}</span>
        <span class="grow" />
        <span class="shrink-0 font-mono text-11 text-dimmed">{{
          typeof problem.code === "string" ? problem.code : problem.code?.value
        }}</span>
      </div>
    </div>
    <template v-if="card">
      <div v-if="identified" class="flex flex-col gap-1" :class="look.section">
        <div class="flex min-w-0 items-center gap-2">
          <UBadge v-if="card.head" :label="card.head" color="info" size="sm" :style="headStyle" />
          <span class="min-w-0 font-mono text-13 font-semibold break-all text-highlighted">{{
            card.word || card.key
          }}</span>
          <span class="grow" />
          <UBadge v-if="card.stale" label="outdated script_docs" color="warning" size="sm" />
          <UBadge v-if="overrides" label="Overrides the game" color="purple" size="sm" />
        </div>
        <div v-if="card.scope" class="font-mono text-11 text-dimmed">
          {{ card.scope.this || "unknown" }} scope<template v-if="card.scope.reason"
            >, {{ card.scope.reason }}</template
          >
        </div>
      </div>
      <div v-if="facts.length || color" :class="look.section">
        <!-- a value wraps to three lines, the rest on its tooltip -->
        <div class="grid grid-cols-[78px_minmax(0,1fr)] items-baseline gap-x-2.5 gap-y-1 text-12">
          <template v-for="[label, value] in facts" :key="label">
            <span class="text-dimmed">{{ label }}</span>
            <span class="flex min-w-0 items-start gap-2 font-mono text-11 text-toned">
              <span class="line-clamp-3 min-w-0 break-words" :title="value">{{ value }}</span>
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
      <div v-if="card.name || card.prose || card.about" class="flex flex-col gap-1.5" :class="look.section">
        <LocQuote v-if="card.name" :text="card.name" />
        <p v-if="card.prose" class="whitespace-pre-line text-12 text-toned">{{ card.prose }}</p>
        <p v-if="card.about" class="text-11 text-dimmed">{{ card.about }}</p>
      </div>
      <div v-if="groups.length" :class="look.section">
        <!-- each origin's chip, under it its first files once each: the line, or how many places it holds (its first
             opens); then how many files more -->
        <div class="grid grid-cols-[78px_minmax(0,1fr)] gap-x-2.5 gap-y-2 text-12">
          <template v-for="(group, i) in groups" :key="i">
            <span class="leading-5 text-dimmed">{{ group.part }}</span>
            <span class="flex min-w-0 flex-col items-start gap-0.5">
              <OriginChip :label="store.originName(group.origin)" :color="store.originColor(group.origin)" small />
              <button
                v-for="file in group.files.slice(0, look.shown)"
                :key="file.rel"
                type="button"
                class="cursor-pointer text-start font-mono text-11 break-all text-muted hover:text-highlighted hover:underline"
                @click="open(file.first)"
              >
                {{ file.count > 1 ? file.rel : `${file.rel}:${file.first.range.start.line + 1}`
                }}<span v-if="file.count > 1" class="font-sans text-dimmed"> · {{ file.count }} places</span>
              </button>
              <span v-if="group.files.length > look.shown" class="text-11 text-dimmed"
                >+{{ group.files.length - look.shown }} more</span
              >
            </span>
          </template>
        </div>
      </div>
      <!-- each key with its word on one line; a narrow card wraps between them -->
      <div
        v-if="identified && look.keys"
        class="flex flex-wrap items-center gap-x-3 gap-y-1 bg-muted px-3.5 py-1.5 text-tiny whitespace-nowrap text-dimmed"
      >
        <span v-for="[key, word] in KEYS" :key="key" class="flex items-center gap-1"
          ><UKbd :value="key" />{{ word }}</span
        >
        <span v-if="card.graphed" class="flex items-center gap-1"><UKbd value="Ctrl Shift G" />graph</span>
        <span class="ms-auto">Right-click for more</span>
      </div>
    </template>
  </div>
</template>
