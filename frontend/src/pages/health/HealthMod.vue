<!-- One mod's health: what it holds, its problems and how much of each language is written. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { groupBy, sortBy } from "es-toolkit";
import { WriteScaffold } from "@services/health/service";
import type { Finding, Overview } from "@services/health/models";
import type { Part } from "@services/lsp/models";
import type { Mod } from "@services/workspace/models";
import {
  countFindings,
  countFindingsBy,
  describeCount,
  joinParts,
  splitSubject,
  list,
  listSiteKeys,
  plural,
  toSiteKey,
} from "../../lib/format";
import { keys, languagesQuery, stateQuery, topicsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import ModAvatar from "../../ui/ModAvatar.vue";
import HealthDetail from "./HealthDetail.vue";
import HealthLink from "./HealthLink.vue";
import HealthList from "./HealthList.vue";
import HealthSection from "./HealthSection.vue";

const {
  mod,
  findings,
  languages,
  overview = undefined,
  log,
  compatibility,
  opened = undefined,
} = defineProps<{
  mod: Mod;
  /** The mod's problems, without its languages. */
  findings: Finding[];
  /** The mod's coverage findings, one for each language besides the base one. */
  languages: Finding[];
  overview?: Overview;
  /** The rows of the game's log. */
  log: Finding[];
  /** The findings of workspace compatibility. */
  compatibility: Finding[];
  /** The open finding, or undefined for none. */
  opened?: Finding;
}>();
const emit = defineEmits<{ open: [finding: Finding]; log: [finding: Finding]; compatibility: [finding: Finding] }>();
const filter = defineModel<string>("filter", { default: "" });

/** How many databases show before the rest fold behind a button. */
const DATABASES_SHOWN = 5;

const workspace = useWorkspaceStore();
const toast = useToast();
const cache = useQueryCache();
const { data: topics } = useQuery(topicsQuery);
const { data: state } = useQuery(stateQuery);
const { data: installLanguages } = useQuery(() => ({
  ...languagesQuery(state.value?.install.id ?? ""),
  enabled: !!state.value?.install.id,
}));
const allDatabases = shallowRef(false);
const topic = shallowRef<string | number>("");

const subtitle = computed(() => {
  const judged = overview?.judged ?? [];
  return [
    mod.descriptor.version && `Version ${mod.descriptor.version}`,
    overview && `judged ${judged.length ? `with ${list(judged.map(workspace.originName))}` : "on its own"}`,
  ]
    .filter(Boolean)
    .join(" · ");
});
/** The finding that the mod is made for another game version, or undefined if it is made for this one. */
const outdated = computed(() => findings.find((finding) => finding.code === "supported-version"));
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
      label: installLanguages.value?.find((installed) => installed.folder === name)?.label ?? name,
      percent: (have / total) * 100,
      text: have === total ? `all ${describeCount(total)}` : `${describeCount(have)} of ${describeCount(total)}`,
      missing: total - have,
    };
  }),
);
/** The topic tabs' items, "All topics" first and then each topic that has a finding. */
const topicItems = computed(() => {
  const counted = countFindingsBy(findings, (finding) => finding.topic);
  return [
    { label: "All topics", value: "" },
    ...(topics.value ?? [])
      .filter(({ id }) => counted[id])
      .map(({ id, label }) => ({ label: `${label} ${describeCount(counted[id] ?? 0)}`, value: id })),
  ];
});
/** The findings of the picked topic, or all of them if no topic is picked. */
const topicFindings = computed(() =>
  topic.value ? findings.filter((finding) => finding.topic === topic.value) : findings,
);
/** The game's log rows by the place they name. */
const logPlaces = computed(() => groupBy(log, toSiteKey));
const hits = computed(() => (opened ? listHits(opened) : []));
/** How many lines of the game's log are at the open problem's place, repeats included. */
const hitLines = computed(() => countFindings(hits.value));
/** The compatibility finding about the file where another mod defines the open problem's name. */
const related = computed(() => {
  const path = opened?.defined?.[0]?.place.path;
  return path
    ? compatibility.find(
        (finding) => finding.code === "removed-names" && finding.sites?.some((site) => site.place.path === path),
      )
    : undefined;
});
/** The related compatibility finding as parts, or none if there is no such finding. */
const relation = computed((): Part[] => {
  const other = related.value;
  return other
    ? [
        ...splitSubject(other),
        { text: ": ", style: "" },
        ...(other.message ?? []),
        { text: " Open it in workspace compatibility.", style: "" },
      ]
    : [];
});

const { mutate: writeScaffold, isLoading: scaffolding } = useMutation({
  mutation: (language: string) => WriteScaffold(mod.id, language),
  onSuccess: (paths) => {
    toast.add({ title: `Wrote ${plural(paths?.length ?? 0, "file")}` });
    void cache.invalidateQueries({ key: keys.health });
  },
});

/** listHits returns the game's log rows at the places of a finding and of the findings folded into it. */
function listHits(finding: Finding): Finding[] {
  return listSiteKeys(finding).flatMap((place) => logPlaces.value[place] ?? []);
}
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
      <UAlert
        v-if="outdated"
        color="warning"
        variant="subtle"
        icon="i-lucide-calendar-clock"
        :title="`Made for game ${outdated.facts?.supported}; the game is ${outdated.facts?.version}.`"
        description="Health judges the mod against the game install you have, so you may see more problems."
        :actions="[
          {
            label: 'Show the version',
            variant: 'ghost',
            color: 'warning',
            onClick: () => outdated && emit('open', outdated),
          },
        ]"
        orientation="horizontal"
      />
    </div>
    <UCollapsible
      v-if="coverage.length"
      class="flex shrink-0 flex-col gap-1.5 px-4 pb-3"
      :ui="{ content: 'flex flex-col gap-1.5' }"
    >
      <button type="button" class="group flex items-center gap-2 self-start text-13 text-highlighted">
        <UIcon
          name="i-lucide-chevron-right"
          class="size-3.5 shrink-0 text-dimmed transition-transform group-data-[state=open]:rotate-90"
        />
        <UIcon name="i-lucide-info" class="size-3.5 shrink-0" style="color: var(--ui-info)" />
        Languages
        <span class="text-dimmed tabular-nums">{{ describeCount(coverage.length) }}</span>
      </button>
      <template #content>
        <div
          v-for="language in coverage"
          :key="language.name"
          class="flex flex-wrap items-center gap-x-2 gap-y-1 ps-11 text-11"
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
      </template>
    </UCollapsible>
    <HealthList
      v-model:filter="filter"
      :findings="topicFindings"
      empty="No problems."
      :opened="opened"
      class="grow"
      @open="emit('open', $event)"
    >
      <template #tabs>
        <UTabs
          v-model="topic"
          :items="topicItems"
          :content="false"
          size="xs"
          variant="pill"
          color="neutral"
          :ui="{ root: 'max-w-full overflow-x-auto', list: 'w-max' }"
        />
      </template>
      <template #trailing="{ finding }">
        <UBadge
          v-if="listHits(finding).length"
          label="in the log"
          color="neutral"
          variant="subtle"
          size="sm"
          class="shrink-0"
        />
      </template>
      <template #detail>
        <HealthDetail v-if="opened" :finding="opened">
          <HealthSection v-if="related" title="Related">
            <HealthLink :level="related.level" :parts="relation" @open="emit('compatibility', related)" />
          </HealthSection>
          <HealthSection v-if="hits[0]" title="In the game's log" :count="hitLines">
            <template #actions>
              <UButton
                label="Show in the log"
                icon="i-lucide-terminal"
                variant="ghost"
                size="xs"
                @click="hits[0] && emit('log', hits[0])"
              />
            </template>
            <p class="font-mono text-11.5 leading-4.5 wrap-anywhere text-toned">
              {{ joinParts(hits[0].message) }}
            </p>
          </HealthSection>
        </HealthDetail>
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </HealthList>
  </div>
</template>
