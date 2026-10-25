<!--
  The Health page: the section cards with their counts, the load order, the table of the picked section, and the
  detail beside it.
-->
<script setup lang="ts">
import { computed, onActivated, shallowRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { isEqual } from "es-toolkit";
import { useRouter } from "vue-router";
import type { TableColumn } from "@nuxt/ui";
import { WriteScaffold } from "@services/health/service";
import type { Overview, Section } from "@services/health/models";
import { ReorderMods } from "@services/workspace/service";
import { describeLanguages, plural } from "../../lib/format";
import {
  errorLogQuery,
  graphDefaultsQuery,
  keys,
  languagesQuery,
  overviewQuery,
  reportQuery,
  stateQuery,
} from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import LoadOrder from "../../ui/LoadOrder.vue";
import OriginChip from "../../ui/OriginChip.vue";
import SectionHead from "../../ui/SectionHead.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import HealthDetail from "./HealthDetail.vue";
import HealthTable from "./HealthTable.vue";

const {
  id,
  section = "",
  row = "",
  name = "",
} = defineProps<{ id: string; section?: string; row?: string; name?: string }>();

const GROUPS = [
  { title: "Load order", hint: "Changes when you reorder mods", sections: ["contests", "overrides", "dependencies"] },
  { title: "Problems in the mods", hint: "Fixed in the files", sections: ["dangling", "localization", "script"] },
  { title: "Beyond the files", hint: "The game's log and each mod's contents", sections: ["errorlog", "overview"] },
];
/** The dot of each card, by Go's section id. */
const DOTS: Record<string, string> = {
  contests: "var(--orange)",
  overrides: "var(--blue)",
  dependencies: "var(--fg-3)",
  dangling: "var(--red)",
  localization: "color-mix(in oklab, var(--orange), var(--accent))",
  script: "var(--orange)",
  errorlog: "var(--red)",
  overview: "var(--fg-3)",
};
// the table's share of the split, in percent, either pane taking up to four fifths; the splitter keeps it
const PANES = [{ slot: "table", defaultSize: 65, minSize: 20, maxSize: 80 }, { slot: "detail" }];

const router = useRouter();
const toast = useToast();
const cache = useQueryCache();
const workspace = useWorkspaceStore();

const { data: state } = useQuery(stateQuery);
const mods = computed(() => state.value?.mods ?? []);
const ids = computed(() => mods.value.map((mod) => mod.id));

// the load order dragged to preview; none dragged is the workspace's own
const preview = shallowRef<string[]>([]);
const order = computed({
  get: () => (preview.value.length ? preview.value : ids.value),
  set: (next: string[]) => (preview.value = next),
});
const dirty = computed(() => !isEqual(order.value, ids.value));

const {
  data: report,
  error,
  isLoading: reportLoading,
  refetch,
} = useQuery(() => ({
  ...reportQuery(order.value),
  enabled: workspace.isOpen() && !!state.value,
}));
onActivated(() => {
  if (report.value) void refetch();
});
const { data: defaults } = useQuery(() => ({ ...graphDefaultsQuery, enabled: workspace.isOpen() }));
const { data: languages } = useQuery(() => ({
  ...languagesQuery(state.value?.install.id ?? ""),
  enabled: !!state.value?.install.id,
}));
const { data: errorLog, isLoading: logLoading } = useQuery(() => ({
  ...errorLogQuery,
  enabled: workspace.isOpen(),
}));

const cards = computed(() => report.value?.sections ?? []);
const current = computed(() =>
  name ? undefined : (cards.value.find((card) => card.id === section) ?? cards.value[0]),
);
// a name from the editor lists every finding that names it, in any section
const rows = computed(() =>
  current.value?.id === "errorlog"
    ? (errorLog.value ?? [])
    : (report.value?.findings ?? []).filter((finding) =>
        name ? finding.name === name : finding.section === current.value?.id,
      ),
);
const title = computed((): Section => current.value ?? { id: "", label: name, note: "", count: 0, tallies: null });
const open = computed(() => (row ? rows.value[Number(row)] : undefined));

const { data: overview } = useQuery(() => ({
  ...overviewQuery,
  enabled: workspace.isOpen() && current.value?.id === "overview",
}));
const overviewColumns = computed((): TableColumn<Overview>[] => [
  { id: "mod", header: "Mod", accessorFn: (entry) => workspace.originName(entry.mod) },
  { accessorKey: "files", header: "Files" },
  { accessorKey: "messages", header: "Messages" },
  ...[...new Set((overview.value ?? []).flatMap((entry) => Object.keys(entry.counts ?? {})))]
    .sort()
    .map((database) => ({
      id: database,
      header: defaults.value?.labels?.[database] ?? database,
      accessorFn: (entry: Overview) => entry.counts?.[database] ?? 0,
    })),
]);

const { mutate: reorder, isLoading: saving } = useMutation({
  mutation: (next: string[]) => ReorderMods(id, next),
  onSuccess: () => {
    preview.value = [];
  },
});

const scaffoldMod = shallowRef<string>();
const scaffoldLanguage = shallowRef<string>();
const { mutate: writeScaffold, isLoading: scaffolding } = useMutation({
  mutation: (target: { mod: string; language: string }) => WriteScaffold(target.mod, target.language),
  onSuccess: (paths) => {
    toast.add({ title: `Wrote ${plural(paths?.length ?? 0, "file")}` });
    void cache.invalidateQueries({ key: keys.health });
  },
});

watch([() => id, () => ids.value.join()], () => {
  preview.value = [];
  scaffoldMod.value = undefined;
  scaffoldLanguage.value = undefined;
});

const facts = computed(() => [
  ...(report.value ? [{ label: plural(report.value.total, "finding"), value: "" }] : []),
  { label: "Preview order", value: dirty.value ? "differs from the workspace" : "matches the workspace" },
]);

/** Shows the section with the given id, with no row open. */
function openSection(sectionId: string): void {
  void router.replace({ query: { section: sectionId } });
}

/** Opens the row with the given index. */
function openRow(index: number): void {
  void router.replace({ query: { section: current.value?.id, row: String(index), name: name || undefined } });
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden px-6 pt-4.5">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" />

    <div class="flex items-center gap-2">
      <span class="me-1.5 shrink-0 text-12 font-medium whitespace-nowrap text-muted">Load order</span>
      <LoadOrder v-model:order="order" :mods="mods" editable class="min-w-0" />
      <span class="ms-2 shrink-0 text-12 whitespace-nowrap text-dimmed">Drag to preview another order.</span>
      <span class="grow" />
      <UButton
        label="Save as workspace order"
        class="shrink-0"
        :disabled="!dirty"
        :loading="saving"
        @click="reorder(order)"
      />
    </div>

    <div class="flex items-stretch gap-6">
      <template v-for="(group, position) in GROUPS" :key="group.title">
        <USeparator v-if="position" orientation="vertical" class="h-auto" />
        <div class="flex min-w-0 flex-1 flex-col gap-2">
          <div class="flex items-baseline gap-2.5 whitespace-nowrap">
            <span class="text-13 font-semibold">{{ group.title }}</span>
            <span class="text-12 text-dimmed">{{ group.hint }}</span>
          </div>
          <div class="grid grid-cols-3 gap-2.5">
            <button
              v-for="card in cards.filter((entry) => group.sections.includes(entry.id))"
              :key="card.id"
              type="button"
              class="flex min-w-0 flex-col gap-1 rounded-lg border px-3.5 py-3 text-left"
              :class="
                current?.id === card.id
                  ? 'border-primary bg-elevated ring-2 ring-(--accent-soft)'
                  : 'border-default bg-muted'
              "
              @click="openSection(card.id)"
            >
              <span class="flex items-center gap-1.5 text-12 font-medium whitespace-nowrap text-muted">
                <span
                  class="shrink-0 rounded-full"
                  :class="position ? 'size-2' : 'size-1.5'"
                  :style="{ background: DOTS[card.id] }"
                />{{ card.label }}
              </span>
              <span class="text-28 tabular-nums">{{ card.count }}</span>
              <span class="truncate text-11 leading-3.75 text-dimmed">
                {{ card.tallies?.map((tally) => `${tally.count} ${tally.kind}`).join(", ") || card.note }}
              </span>
            </button>
          </div>
        </div>
      </template>
    </div>

    <div v-if="current?.id === 'localization'" class="flex items-center gap-2">
      <span class="text-12 font-medium whitespace-nowrap text-muted">Write the files a mod lacks</span>
      <USelect
        :model-value="scaffoldMod ?? ids[0]"
        :items="ids.map((mod) => ({ label: workspace.originName(mod), value: mod }))"
        size="sm"
        class="w-48"
        @update:model-value="scaffoldMod = $event"
      />
      <USelect
        :model-value="scaffoldLanguage ?? state?.language"
        :items="describeLanguages(languages)"
        size="sm"
        class="w-36"
        @update:model-value="scaffoldLanguage = $event"
      />
      <UButton
        label="Write scaffold"
        :loading="scaffolding"
        @click="writeScaffold({ mod: scaffoldMod ?? ids[0], language: scaffoldLanguage ?? state?.language ?? '' })"
      />
    </div>

    <section
      v-if="current?.id === 'overview'"
      class="flex min-h-0 grow flex-col overflow-hidden rounded-t-lg border border-b-0 border-default bg-muted"
    >
      <SectionHead :title="current.label" :subtitle="current.note" :count="overview?.length" />
      <UTable :data="overview ?? []" :columns="overviewColumns" sticky empty="No mods." class="min-h-0 flex-1">
        <template #mod-cell="{ row: entry }"><OriginChip :origin="entry.original.mod" /></template>
      </UTable>
    </section>
    <USplitter
      v-else-if="current || name"
      :items="PANES"
      auto-save-id="pmt.health"
      class="min-h-0 grow overflow-hidden rounded-t-lg border border-b-0 border-default"
    >
      <template #table>
        <HealthTable
          :key="title.label"
          :findings="rows"
          :section="title"
          :opened="row ? Number(row) : -1"
          :loading="reportLoading || logLoading"
          class="min-w-0 flex-1 border-e border-default"
          @open="openRow"
        />
      </template>
      <template #detail>
        <HealthDetail v-if="open" :finding="open" />
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </USplitter>

    <StatusFacts :facts="facts" />
  </div>
</template>
