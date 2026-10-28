<!--
  One mod's health, with what it holds, its problems and how much of each language is written. The open problem's
  detail links to the game's log lines at its place.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQueryCache } from "@pinia/colada";
import { groupBy, sortBy } from "es-toolkit";
import { WriteScaffold } from "@services/health/service";
import type { Finding, Overview } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import {
  countLines,
  describeCount,
  describeLanguages,
  describeLogLine,
  list,
  listFindingRows,
  plural,
  toSiteKey,
} from "../../lib/format";
import { keys } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import ModAvatar from "../../ui/ModAvatar.vue";
import HealthDetail from "./HealthDetail.vue";
import HealthList from "./HealthList.vue";

const {
  mod,
  findings,
  languages,
  overview = undefined,
  log,
  opened = -1,
} = defineProps<{
  mod: Mod;
  /** The mod's problems, without its languages. */
  findings: Finding[];
  /** The mod's coverage findings, one for each language besides the base one. */
  languages: Finding[];
  overview?: Overview;
  /** The rows of the game's log. */
  log: Finding[];
  /** The open finding's index, or -1 for none. */
  opened?: number;
}>();
const emit = defineEmits<{ open: [index: number]; log: [index: number] }>();
const filter = defineModel<string>("filter", { default: "" });

/** How many databases show before the rest fold behind a button. */
const DATABASES_SHOWN = 5;

const workspace = useWorkspaceStore();
const toast = useToast();
const cache = useQueryCache();
const allDatabases = shallowRef(false);

const open = computed(() => findings[opened]);
const subtitle = computed(() => {
  const dependencies = mod.descriptor.dependencies ?? [];
  return [
    mod.descriptor.version && `Version ${mod.descriptor.version}`,
    dependencies.length && `depends on ${list(dependencies.map((one) => one.title || one.id))}`,
  ]
    .filter(Boolean)
    .join(" · ");
});
/** The mod's counts by database, the largest first. */
const databases = computed(() =>
  sortBy(
    Object.entries(overview?.counts ?? {}).flatMap(([label, count]) => (count ? [{ count, label }] : [])),
    [(database) => -database.count],
  ),
);
const facts = computed(() => [
  ...(overview
    ? [
        { count: overview.files, label: "files" },
        { count: overview.messages, label: "localization keys" },
      ]
    : []),
  ...databases.value.slice(0, allDatabases.value ? undefined : DATABASES_SHOWN),
]);
const coverage = computed(() =>
  languages.map((language) => {
    const have = Number(language.facts?.have);
    const total = Number(language.facts?.of);
    const name = language.facts?.name ?? "";
    return {
      name,
      label: describeLanguages([name])[0]?.label ?? name,
      percent: (have / total) * 100,
      text: have === total ? `all ${describeCount(total)}` : `${describeCount(have)} of ${describeCount(total)}`,
      missing: total - have,
    };
  }),
);
/** The game's log rows by the place they name, each with its index. */
const logPlaces = computed(() => groupBy(listFindingRows(log), (row) => toSiteKey(row.finding)));
const hits = computed(() => (open.value ? (logPlaces.value[toSiteKey(open.value)] ?? []) : []));
const hit = computed(() => {
  const first = hits.value[0];
  return (
    first && {
      index: first.index,
      message: describeLogLine(first.finding),
      times: countLines(hits.value.map((one) => one.finding)),
    }
  );
});

const { mutate: writeScaffold, isLoading: scaffolding } = useMutation({
  mutation: (language: string) => WriteScaffold(mod.id, language),
  onSuccess: (paths) => {
    toast.add({ title: `Wrote ${plural(paths?.length ?? 0, "file")}` });
    void cache.invalidateQueries({ key: keys.health });
  },
});
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col">
    <div class="flex shrink-0 flex-col gap-2.5 px-4 pt-3.5 pb-3">
      <div class="flex items-center gap-3">
        <ModAvatar :mod="mod" size="lg" />
        <span class="flex min-w-0 flex-col">
          <span class="truncate text-16 text-highlighted">{{ mod.descriptor.name }}</span>
          <span v-if="subtitle" class="truncate text-11 text-dimmed">{{ subtitle }}</span>
        </span>
        <span class="grow" />
        <UButton
          label="Publish"
          icon="i-lucide-upload"
          variant="ghost"
          color="neutral"
          :to="{ name: 'release', params: { id: workspace.activeId() }, query: { mod: mod.id } }"
        />
        <UButton
          label="Open in the IDE"
          icon="i-lucide-code"
          variant="ghost"
          color="neutral"
          :to="{ name: 'workspace-ide', params: { id: workspace.activeId() } }"
        />
      </div>
      <div class="flex flex-wrap items-center gap-1.5 text-12 text-muted">
        <span v-for="fact in facts" :key="fact.label" class="rounded-chip border border-default bg-muted px-2 py-0.5">
          <b class="font-semibold text-highlighted tabular-nums">{{ describeCount(fact.count) }}</b> {{ fact.label }}
        </span>
        <UButton
          v-if="!allDatabases && databases.length > DATABASES_SHOWN"
          :label="plural(databases.length - DATABASES_SHOWN, 'more kind')"
          variant="ghost"
          color="neutral"
          size="xs"
          @click="allDatabases = true"
        />
      </div>
    </div>
    <HealthList
      v-model:filter="filter"
      topics
      :info-check="languages[0]?.code ?? ''"
      :findings="findings"
      empty="No problems."
      :opened="opened"
      class="grow"
      @open="emit('open', $event)"
    >
      <template #trailing="{ finding }">
        <UBadge
          v-if="logPlaces[toSiteKey(finding)]"
          label="in the log"
          color="neutral"
          variant="subtle"
          size="sm"
          class="shrink-0"
        />
      </template>
      <template #info>
        <div class="flex items-start gap-2.5 px-3 py-2.5">
          <UIcon name="i-lucide-info" class="mt-0.75 size-3.5 shrink-0" style="color: var(--ui-info)" />
          <div class="flex min-w-0 grow flex-col gap-1.5">
            <span class="text-13 text-highlighted">Languages</span>
            <div
              v-for="language in coverage"
              :key="language.name"
              class="flex flex-wrap items-center gap-x-2 gap-y-1 text-11"
            >
              <span class="w-36 shrink-0 truncate text-toned">{{ language.label }}</span>
              <UProgress :model-value="language.percent" size="xs" color="info" class="w-24 shrink-0" />
              <span class="shrink-0 text-dimmed tabular-nums">{{ language.text }}</span>
              <span class="grow" />
              <UButton
                v-if="language.missing"
                :label="`Write the ${plural(language.missing, `missing ${language.label} key`)}`"
                size="xs"
                :loading="scaffolding"
                @click="writeScaffold(language.name)"
              />
            </div>
          </div>
        </div>
      </template>
      <template #detail>
        <HealthDetail v-if="open" :finding="open" :findings="findings">
          <button
            v-if="hit"
            type="button"
            class="flex items-center gap-2.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-start hover:bg-accented"
            @click="emit('log', hit.index)"
          >
            <UIcon name="i-lucide-terminal" class="size-4 shrink-0 text-dimmed" />
            <span class="flex min-w-0 grow flex-col">
              <span class="text-12 text-highlighted">In the game's log, {{ plural(hit.times, "time") }}</span>
              <span class="truncate font-mono text-11 text-dimmed">{{ hit.message }}</span>
            </span>
            <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
          </button>
        </HealthDetail>
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </HealthList>
  </div>
</template>
