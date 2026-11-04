<!--
  The first-mod checklist on the Library's active card: five steps from making a mod to a Workshop listing for it, each
  done by a fact about the workspace's mods. It shows while a step is left, until it is hidden.
-->
<script setup lang="ts">
import { computed, onActivated, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { keyBy } from "es-toolkit";
import type { RouteLocationRaw } from "vue-router";

import { describeDumps, list } from "../lib/format";
import { levelsQuery, overviewQuery, ownListingsQuery, reportQuery, stateQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import ScriptDocsGuide from "./ScriptDocsGuide.vue";

/** The steps, in order, with the button of each that is not done. */
const STEPS = [
  { id: "make", title: "Make the mod" },
  { id: "file", title: "Write a file of your own" },
  { id: "errors", title: "Clear your mod's errors", button: "Open Health", icon: "i-lucide-activity" },
  { id: "knowledge", title: "Get the game's own knowledge", button: "Show me how" },
  { id: "listing", title: "Write the Workshop listing", button: "Open Publish", icon: "i-lucide-upload" },
] as const;

const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const guiding = shallowRef(false);

const record = computed(() => workspace.active()?.record);
const mods = computed(() => workspace.listWritableMods());
const wanted = computed(
  () => settings.isGuided() && !record.value?.hideChecklist && workspace.isOpen() && !!mods.value.length,
);

const { data: state } = useQuery(stateQuery);
const { data: levels } = useQuery(levelsQuery);
const { data: report, refresh: refreshReport } = useQuery(() => ({
  ...reportQuery(workspace.listModIds()),
  enabled: wanted.value,
}));
const { data: overview, refresh: refreshOverview } = useQuery(() => ({ ...overviewQuery, enabled: wanted.value }));
const { data: listings, refresh: refreshListings } = useQuery(() => ({
  ...ownListingsQuery(mods.value.map((mod) => mod.id)),
  enabled: wanted.value,
}));

const rows = computed(() => {
  const dumps = state.value && describeDumps(state.value.dumps);
  const listed = listings.value;
  if (!wanted.value || !levels.value || !report.value || !overview.value || !listed || !dumps) return [];
  const groups = keyBy(report.value.mods ?? [], (group) => group.mod);
  const fixes = levels.value.filter((level) => level.fix);
  const trouble = mods.value.flatMap((mod) => {
    const phrases = fixes.flatMap((level) => groups[mod.id]?.phrases?.[level.id] ?? []);
    return phrases.length ? [`Health finds ${list(phrases)} in ${workspace.originName(mod.id)}.`] : [];
  });
  const defined = overview.value.flatMap((one) =>
    mods.value.some((mod) => mod.id === one.mod)
      ? Object.entries(one.counts ?? {}).flatMap(([name, count]) => (count && count > 0 ? name : []))
      : [],
  );
  const unlisted = mods.value.filter((_, index) => {
    const listing = listed[index];
    return !listing?.description || !listing.thumbnail;
  });
  const facts: Record<
    (typeof STEPS)[number]["id"],
    { done: boolean; note: string; to?: RouteLocationRaw; onSelect?: () => void }
  > = {
    make: { done: true, note: mods.value[0]?.dir ?? "" },
    file: defined.length
      ? { done: true, note: `Your mod defines ${list([...new Set(defined)])}.` }
      : { done: false, note: "A trait, an event or a decision, in a file of your mod." },
    errors: trouble.length
      ? {
          done: false,
          note: `${trouble.join(" ")} An error is a line the game skips.`,
          to: { name: "health", params: { id: workspace.activeId() } },
        }
      : { done: true, note: "" },
    knowledge: dumps.current
      ? { done: true, note: "" }
      : {
          done: false,
          note: `It may be older than ${state.value?.install.version || "the game"}.`,
          onSelect: () => (guiding.value = true),
        },
    listing: unlisted.length
      ? {
          done: false,
          note: "A description and a thumbnail, for when you publish.",
          to: { name: "release", params: { id: workspace.activeId() }, query: { mod: unlisted[0]?.id } },
        }
      : { done: true, note: "" },
  };
  return STEPS.map((step) => ({ ...step, ...facts[step.id] }));
});
const current = computed(() => rows.value.find((row) => !row.done));
const done = computed(() => rows.value.filter((row) => row.done).length);

// the Library stays mounted behind the other pages, so what the steps are judged by is read again when it is shown
onActivated(() => {
  if (wanted.value) void Promise.all([refreshReport(), refreshOverview(), refreshListings()]);
});

/** hide takes the checklist off this workspace's card. */
function hide(): void {
  if (record.value) workspace.update({ ...record.value, hideChecklist: true });
}
</script>

<template>
  <section
    v-if="current"
    class="flex flex-col rounded-lg border border-default bg-muted px-3.5 pt-3 pb-1.5"
    aria-label="Your first mod"
  >
    <div class="flex items-center gap-2 pb-1.5">
      <h2 class="text-13 font-semibold">Your first mod</h2>
      <span class="text-12 text-dimmed">{{ done }} of {{ rows.length }}</span>
      <span class="flex gap-0.75">
        <span
          v-for="row in rows"
          :key="row.id"
          class="size-1.5 rounded-full"
          :class="row.done ? 'bg-success' : 'bg-accented'"
        />
      </span>
      <span class="grow" />
      <UButton label="Hide" variant="ghost" color="neutral" size="xs" @click="hide" />
    </div>
    <div
      v-for="(row, index) in rows"
      :key="row.id"
      class="flex min-h-11 items-center gap-3 border-t border-default first:border-t-0"
    >
      <span
        class="flex size-5.5 shrink-0 items-center justify-center rounded-full text-11 font-medium"
        :class="
          row.done
            ? 'bg-success text-inverted'
            : row.id === current.id
              ? 'text-highlighted ring-2 ring-primary'
              : 'bg-accented text-toned'
        "
      >
        <UIcon v-if="row.done" name="i-lucide-check" class="size-3" />
        <template v-else>{{ index + 1 }}</template>
      </span>
      <div class="flex min-w-0 grow flex-col gap-px">
        <span class="text-13" :class="row.done ? 'text-toned' : 'font-medium'">{{ row.title }}</span>
        <span v-if="row.note" class="truncate text-12 text-dimmed" :class="{ 'font-mono': row.id === 'make' }">
          {{ row.note }}
        </span>
      </div>
      <UButton
        v-if="!row.done && 'button' in row"
        :label="row.button"
        :icon="'icon' in row ? row.icon : undefined"
        :to="row.to"
        size="sm"
        :color="row.id === current.id ? 'primary' : 'neutral'"
        :variant="row.id === current.id ? 'solid' : 'outline'"
        @click="row.onSelect?.()"
      />
    </div>
    <ScriptDocsGuide v-model:open="guiding" :game="state?.game?.id ?? ''" />
  </section>
</template>
