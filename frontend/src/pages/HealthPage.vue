<script setup lang="ts">
/**
 * Health: what the workspace's load order decides and what it lacks. Five counts pick the table;
 * a row opens beside it with the lines each of its sites writes.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useRoute } from "vue-router";
import { useSortable } from "@vueuse/integrations/useSortable";
import WorkspaceToolBar from "../components/WorkspaceToolBar.vue";
import LanguageHealthStrip from "../components/LanguageHealthStrip.vue";
import DetailPane from "../components/DetailPane.vue";
import ScriptSnippet from "../components/ScriptSnippet.vue";
import HealthTable, {
  PROBLEM_CARDS,
  problemsOf,
  type Card,
  type Gap,
  type ProblemCard,
  type Row,
} from "../components/health/HealthTable.vue";
import { useHealth } from "../composables/useHealth";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import { useOpenInIde } from "../composables/useOpenInIde";
import { useWorkspaceStore } from "../stores/workspace";

defineOptions({ name: "HealthPage" });

const PANEL_UI = { root: "min-h-0", body: "gap-0 overflow-hidden p-0 sm:p-0" } as const;

const route = useRoute();
const store = useWorkspaceStore();
const { openInIde } = useOpenInIde();
const workspaceId = computed(() => String(route.params.id ?? ""));
const live = useLiveEnabled(workspaceId);
const { order, dirty, health, error, isPending, save, saving, locKey, lookup, places, lines } = useHealth(
  workspaceId,
  live,
);

const card = shallowRef<Card>("contests");
const selected = shallowRef<Row | null>(null);
// the rows the table shows for its card, which that card's numbers count
const shown = shallowRef<{ card: Card; rows: Row[] }>({ card: "contests", rows: [] });
watch(card, () => (selected.value = null));

// the playset's chips, dragged to preview another load order
useSortable(useTemplateRef<HTMLElement>("chips"), order, { animation: 150, watchElement: true });

/** A card's rows: those its table shows, so its numbers follow the filters; all when not chosen. */
function listed<T>(id: Card, all: T[] | null | undefined): T[] {
  return card.value === id && shown.value.card === id ? (shown.value.rows as T[]) : (all ?? []);
}

const cards = computed(() => {
  const report = health.value;
  const contests = listed("contests", report?.contests);
  const firstWins = contests.filter((contest) => contest.firstWins).length;
  const depends = listed("depends", report?.depends);
  const languages = new Map<string, number>();
  if (card.value === "loc" && shown.value.card === "loc") {
    for (const gap of shown.value.rows as Gap[]) languages.set(gap.language, (languages.get(gap.language) ?? 0) + 1);
  } else {
    for (const language of report?.loc ?? []) {
      languages.set(language.language, language.untranslated?.length ?? 0);
    }
  }
  return [
    {
      id: "contests",
      label: "Conflicts",
      color: "warning",
      count: contests.length,
      note: `entries two mods both define · ${contests.length - firstWins} last-loaded wins · ${firstWins} first-loaded wins`,
    },
    {
      id: "overrides", label: "Overrides", color: "secondary",
      count: listed("overrides", report?.overrides).length,
      note: "game entries a mod replaces or adds to",
    },
    {
      id: "depends", label: "Dependencies", color: "info",
      count: depends.length,
      note: `mods using entries only another mod adds · ${depends.filter((row) => row.affected).length} affected by changes`,
    },
    {
      id: "dangling", label: "Dangling refs", color: "error",
      count: listed("dangling", report?.dangling).length,
      note: "names used but defined nowhere",
    },
    {
      id: "loc",
      label: "Loc gaps",
      color: "primary",
      count: [...languages.values()].reduce((sum, count) => sum + count, 0),
      note: `keys missing in: ${[...languages].map(([language, count]) => `${language} ${count}`).join(" · ")}`,
    },
    ...(Object.keys(PROBLEM_CARDS) as ProblemCard[]).map((id) => ({
      id, label: PROBLEM_CARDS[id].label, color: "neutral",
      count: listed(id, problemsOf(report, id)).length,
      note: PROBLEM_CARDS[id].note,
    })),
  ] satisfies { id: Card; label: string; color: string; count: number; note: string }[];
});
const current = computed(() => cards.value.find((item) => item.id === card.value));

// the selected row's sites with their files and the lines read for them, else the one place the
// row is written
const sites = computed(() => {
  const row = selected.value;
  const sources = health.value?.sources ?? [];
  if (!row) return [];
  if ("sites" in row) {
    return (row.sites ?? []).map((site, i) => ({ ...site, at: sources[site.file], lines: lines.value?.[i] ?? null }));
  }
  if ("site" in row) return [{ ...row.site, at: sources[row.site.file], lines: lines.value?.[0] ?? null }];
  return [{ at: sources[row.file], line: row.line, lines: null, database: undefined, name: undefined }];
});
const kept = computed(() => (selected.value && "winner" in selected.value ? selected.value.winner : -1));
// the open row when it is a contest, whose kind and rule head the pane
const contest = computed(() => (selected.value && "rule" in selected.value ? selected.value : null));
// what its rule does: an inject adds to the kept definition, a mode like REPLACE takes its place,
// else load order decides
const ruleText = computed(() => {
  const row = contest.value;
  if (!row) return "";
  if (row.sites?.some((site) => site.injected)) return "an injected definition is added to the one kept";
  if (row.rule !== "FIOS" && row.rule !== "LIOS") return `the ${row.rule} definition takes its place`;
  return `the ${row.firstWins ? "first" : "last"} definition loaded wins`;
});
const title = computed(() => {
  const row = selected.value;
  if (!row) return "";
  if ("message" in row) return row.message;
  if ("key" in row) return row.key;
  if ("name" in row) return row.name;
  return `${store.originName(row.from)} ${row.affected ? "is affected by" : "depends on"} ${store.originName(row.to)}`;
});
// the mod an open dependency row is about, which a removed site says took the name out
const remover = computed(() => (selected.value && "to" in selected.value ? store.originName(selected.value.to) : ""));
// an open loc row's key is looked up; an open row's sites have their lines read, a definition's
// whole
watch(selected, (row) => {
  const sources = health.value?.sources ?? [];
  locKey.value = row && "gap" in row ? row.key : "";
  const written = row && "site" in row ? [row.site] : row && "sites" in row ? (row.sites ?? []) : [];
  places.value = written.map((site) => ({ path: sources[site.file].path, line: site.line, col: site.col, definition: !!row && "rule" in row }));
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <WorkspaceToolBar :workspace-id="workspaceId">
      <template #trailing>
        <LanguageHealthStrip v-if="workspaceId" :workspace-id="workspaceId" />
      </template>
    </WorkspaceToolBar>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />

    <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-default px-3 py-2 text-sm text-muted">
      <span class="font-medium text-toned">Load order</span>
      <ul ref="chips" class="flex flex-wrap items-center gap-1.5">
        <li
          v-for="id in order"
          :key="id"
          class="flex cursor-grab items-center gap-1.5 rounded-md border border-default bg-elevated py-1 pr-2.5 pl-1
            text-default hover:border-accented"
        >
          <UIcon name="i-lucide-grip-vertical" class="size-4 text-dimmed" />
          <img v-if="store.thumbUrls[id]" :src="store.thumbUrls[id]" alt="" class="size-5 rounded-sm object-cover" />
          <span class="size-2 shrink-0 rounded-full" :style="{ backgroundColor: store.originColor(id) }" />
          {{ store.originName(id) }}
        </li>
      </ul>
      <span>drag to preview another order</span>
      <div class="flex-1" />
      <UButton label="Save as workspace order" size="sm" :disabled="!dirty" :loading="saving" @click="save()" />
    </div>

    <div class="grid shrink-0 grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-2 p-2">
      <button
        v-for="item in cards"
        :key="item.id"
        type="button"
        class="rounded-lg border bg-elevated px-3 py-2.5 text-left transition-colors"
        :class="card === item.id ? 'border-(--card) ring-1 ring-(--card)' : 'border-default hover:border-accented'"
        :style="{ '--card': `var(--ui-${item.color})` }"
        @click="card = item.id"
      >
        <div class="text-sm font-medium text-toned">{{ item.label }}</div>
        <div class="my-0.5 text-3xl font-semibold tabular-nums text-(--card)">{{ item.count }}</div>
        <div class="truncate text-xs text-muted">{{ item.note }}</div>
      </button>
    </div>

    <UDashboardGroup
      storage="local"
      storage-key="pmt-health"
      :ui="{ base: 'relative inset-auto flex min-h-0 min-w-0 flex-1 overflow-hidden' }"
      class="mx-2 mb-2 rounded-lg border border-default"
    >
      <UDashboardPanel id="health-table" resizable :default-size="65" :min-size="40" :max-size="85" :ui="PANEL_UI">
        <HealthTable
          :health="health"
          :card="card"
          :loading="isPending"
          @select="selected = $event"
          @shown="(id, rows) => (shown = { card: id, rows })"
        >
          <span class="text-base font-semibold text-highlighted">{{ current?.label }}</span>
          <span class="truncate text-sm text-muted">{{ current?.note }}</span>
        </HealthTable>
      </UDashboardPanel>
      <UDashboardPanel id="health-sites" :ui="PANEL_UI">
        <DetailPane
          :workspace-id="workspaceId"
          :title="title"
          :file="sites[kept]?.at.path ?? sites[0]?.at.path"
          :rel="sites[kept]?.at.rel ?? sites[0]?.at.rel"
          :line="sites[kept]?.line ?? sites[0]?.line"
        >
          <template v-if="contest" #badges>
            <UBadge :label="contest.database" color="neutral" variant="subtle" />
            <UBadge :label="contest.rule" :color="contest.firstWins ? 'warning' : 'info'" variant="subtle" />
            <span class="text-sm text-toned">{{ ruleText }}</span>
          </template>
          <div class="space-y-3 text-sm">
            <!-- a site without lines, as a loc row's, is the header's own file and not repeated -->
            <template v-for="(site, i) in sites" :key="`${site.at.path}:${site.line}`">
              <div v-if="site.lines?.length" class="overflow-hidden rounded-md border border-default">
                <div class="flex items-center gap-2 border-b border-default bg-default px-2 py-1.5">
                  <span
                    class="size-2 shrink-0 rounded-full"
                    :style="{ backgroundColor: store.originColor(site.at.origin) }"
                  />
                  <span class="shrink-0 text-sm font-medium text-highlighted">
                    {{ store.originName(site.at.origin) }}
                  </span>
                  <UBadge v-if="site.database" :label="site.database" color="neutral" variant="subtle" size="sm" />
                  <span v-if="site.name" class="shrink-0 font-mono text-xs text-toned">{{ site.name }}</span>
                  <button
                    type="button"
                    class="min-w-0 truncate font-mono text-xs text-muted hover:underline"
                    @click="openInIde(workspaceId, site.at.path, site.line + 1)"
                  >
                    {{ site.at.rel }}:{{ site.line + 1 }}
                  </button>
                  <UBadge
                    v-if="site.removed"
                    :label="`removed by ${remover}`"
                    color="error"
                    variant="subtle"
                    size="sm"
                    class="ml-auto shrink-0"
                  />
                  <UBadge
                    v-else-if="kept >= 0"
                    :label="i === kept ? 'kept' : site.injected ? 'injected' : 'replaced'"
                    :color="i === kept ? 'success' : site.injected ? 'info' : 'error'"
                    variant="subtle"
                    size="sm"
                    class="ml-auto shrink-0"
                  />
                </div>
                <ScriptSnippet
                  :text="site.lines.join('\n')"
                  language="paradox"
                  :from-line="site.line"
                  gutter
                  class="rounded-none! border-0!"
                />
              </div>
            </template>
            <section v-if="lookup?.entries?.length">
              <h4 class="mb-2 text-sm font-semibold text-highlighted">Text</h4>
              <!-- the text column shrinks to the pane: a long path is cut, its whole on hover -->
              <dl
                class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 rounded-md border border-default bg-default
                  p-3"
              >
                <template v-for="entry in lookup.entries" :key="`${entry.at.path}:${entry.line}`">
                  <dt class="text-muted">{{ entry.language }}</dt>
                  <dd class="min-w-0">
                    <blockquote class="border-l-2 border-accented pl-3 break-words text-default italic">
                      {{ entry.value }}
                    </blockquote>
                    <button
                      type="button"
                      class="mt-1 block max-w-full truncate font-mono text-xs text-muted hover:underline"
                      :title="entry.at.rel"
                      @click="openInIde(workspaceId, entry.at.path, entry.line + 1)"
                    >
                      {{ entry.at.rel }}:{{ entry.line + 1 }}
                    </button>
                  </dd>
                </template>
              </dl>
            </section>
            <div v-if="lookup?.readers?.length" class="space-y-1">
              <h4 class="mb-2 text-sm font-semibold text-highlighted">Read by</h4>
              <button
                v-for="reader in lookup.readers"
                :key="`${reader.at.path}:${reader.line}:${reader.col}`"
                type="button"
                class="block max-w-full truncate font-mono text-xs text-muted hover:underline"
                :title="reader.at.rel"
                @click="openInIde(workspaceId, reader.at.path, reader.line + 1)"
              >
                {{ reader.at.rel }}:{{ reader.line + 1 }}
              </button>
            </div>
          </div>
        </DetailPane>
      </UDashboardPanel>
    </UDashboardGroup>
  </div>
</template>
