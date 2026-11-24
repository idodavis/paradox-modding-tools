<!-- The Atlas page: the browser, one canvas for whatever is picked under its row of filters, and the detail pane. -->
<script setup lang="ts">
import { computed, nextTick, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useFullscreen, useLocalStorage } from "@vueuse/core";
import { isEqual } from "es-toolkit";
import { useRoute, useRouter } from "vue-router";
import type { SplitterItem } from "@nuxt/ui";

import { Type } from "@internal/lang/models";
import type { Filter } from "@services/atlas/models";
import { describeCount, plural, toSentence } from "../../lib/format";
import { catalogsQuery, defaultsQuery, graphQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/toolView";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import AtlasBrowser from "./AtlasBrowser.vue";
import AtlasCanvas from "./AtlasCanvas.vue";
import AtlasDetail from "./AtlasDetail.vue";
import { GOODS, isGroup, type Kind } from "./AtlasNode.vue";
import AtlasStart, { type Recent } from "./AtlasStart.vue";
import InnovationGrid from "./InnovationGrid.vue";
import { labelOf } from "./NodePicture.vue";
import PaintingBand from "./PaintingBand.vue";

const {
  id,
  subject = "",
  node = "",
  conditions = "",
  picked = "",
  view = "",
  direction = "",
} = defineProps<{
  id: string;
  /** The id of the node the canvas is about. */
  subject?: string;
  /** The id of the selected node. */
  node?: string;
  /** The filter's changes from the defaults, as JSON. */
  conditions?: string;
  /** The ids of the production methods picked, joined by commas. */
  picked?: string;
  /** "chain" draws a technology's chain alone. */
  view?: string;
  /** "down" reads a tree top to bottom. */
  direction?: string;
}>();

const PANES: SplitterItem[] = [
  { id: "browser", slot: "browser", defaultSize: 16, minSize: 12, maxSize: 35, collapsible: true, collapsedSize: 0 },
  { id: "view", slot: "view", minSize: 30 },
  { id: "detail", slot: "detail", defaultSize: 24, minSize: 16, maxSize: 45, collapsible: true, collapsedSize: 0 },
];
const DETAIL = PANES.findIndex((pane) => pane.id === "detail");
const RECENT_KEPT = 8;
const REACH_DEPTH = { min: 1, max: 10 };
const REACH_LIMIT = { min: 10, max: 400, step: 10 };
/** The value of the era menu's first choice, every era. A choice of a select cannot have "" as its value. */
const ANY_ERA = "any";
const CHAIN = "chain";
const DOWN = "down";
/** A group written as a plain value, and what is in no group, have no definition and so no detail of their own. */
const WRITTEN = "group:";
const CHECKBOX_UI = { item: "items-center py-1", label: "flex items-center gap-2 text-12 font-normal" };
const BAR =
  "flex h-10 shrink-0 items-center gap-2 overflow-x-auto border-b border-default bg-muted px-3 whitespace-nowrap";

const [DefineOrigins, OriginsMenu] = createReusableTemplate();
const [DefineEnd, BarEnd] = createReusableTemplate();

const route = useRoute();
const router = useRouter();
const workspace = useWorkspaceStore();
const splitter = useTemplateRef("splitter");
const stage = useTemplateRef("stage");
const canvas = useTemplateRef("canvas");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(stage);
const isDetailOpen = useLocalStorage("pmt.atlas.detail", false);
const recent = useLocalStorage<Recent[]>(() => `pmt.atlas.recent.${id}`, []);

const { data: defaults } = useQuery(() => ({ ...defaultsQuery, enabled: workspace.isOpen() }));
const { data: catalogs } = useQuery(() => ({ ...catalogsQuery, enabled: workspace.isOpen() }));
const changes = computed<Partial<Filter>>(() => {
  try {
    return conditions ? JSON.parse(conditions) : {};
  } catch {
    return {};
  }
});
const filter = computed<Filter | undefined>(
  () => defaults.value && { ...defaults.value, ...changes.value, picked: picked ? picked.split(",") : [] },
);
const { data: graph, error } = useQuery(() => ({
  ...graphQuery({ subject, filter: filter.value ?? ({} as Filter) }),
  enabled: workspace.isOpen() && !!subject && !!filter.value,
}));
const drawn = computed(() => (subject ? graph.value : undefined));
const nodes = computed(() => drawn.value?.nodes ?? []);
const root = computed(() => nodes.value.find((drawnNode) => drawnNode.id === drawn.value?.root));
const kind = computed<Kind>(() => {
  const type = root.value?.type;
  if (GOODS.includes(type)) return "good";
  if (type === Type.TypeBuilding || type === Type.TypeProductionMethod) return "building";
  return type === Type.TypeTechnology ? "tree" : "event";
});
/** A tree none of whose technologies requires another is drawn as the grid of era by group. */
const isGrid = computed(() => kind.value === "tree" && !drawn.value?.edges?.length);
/** The group selected on the canvas, and the id whose detail the pane shows: the selection's, else the subject's. */
const group = computed(() => (isGroup(node) ? nodes.value.find((drawnNode) => drawnNode.id === node) : undefined));
const detailId = computed(() => {
  if (!group.value) return node || root.value?.id || "";
  const own = node.slice(node.indexOf(":") + 1);
  return own.startsWith(WRITTEN) || own.endsWith(":") ? "" : own;
});
const opened = computed(() => filter.value?.expand ?? []);
/** Whether a filter differs from the defaults. An opened fold does not count. */
const isChanged = computed(() => Object.keys(changes.value).some((key) => key !== "expand"));

const databases = computed(() =>
  (defaults.value?.databases ?? []).map((database) => ({
    database,
    what: toSentence(defaults.value?.labels?.[database] ?? database),
    count: describeCount(nodes.value.filter((drawnNode) => drawnNode.database === database).length),
  })),
);
const shownDatabases = computed(() => {
  const shown = filter.value?.databases ?? [];
  if (shown.length === databases.value.length) return "everything";
  if (shown.length === 1) return databases.value.find((row) => row.database === shown[0])?.what ?? shown[0];
  return `${shown.length} of ${databases.value.length}`;
});
const origins = computed(() => (defaults.value?.origins ?? []).map((origin) => ({ value: origin })));
const originsLabel = computed(() => {
  const shown = filter.value?.origins ?? [];
  if (shown.length === origins.value.length) return "From: every origin";
  if (shown.length === 1) return `From: ${workspace.originName(shown[0] ?? "")}`;
  return `From: ${shown.length} of ${origins.value.length} origins`;
});
const eras = computed(() => [
  { label: `Any ${drawn.value?.eras?.[0]?.what ?? "era"}`, value: ANY_ERA },
  ...(drawn.value?.eras ?? []).map((era) => ({ label: labelOf(era), value: era.id })),
]);
/** The audiences of an age as the choices of a menu: a group's own row is a heading. */
const audiences = computed(() =>
  (drawn.value?.audiences ?? []).map((audience) =>
    audience.id === audience.group
      ? { type: "label" as const, label: `${audience.title} · ${describeCount(audience.count)}` }
      : { label: audience.title, value: audience.id, count: describeCount(audience.count) },
  ),
);
const tree = computed(() => drawn.value?.trees?.find((listed) => listed.id === root.value?.group));
const technologies = computed(() => nodes.value.filter((drawnNode) => drawnNode.type === Type.TypeTechnology).length);
const leads = computed(() => {
  const { needs = 0, leads: count = 0, sharedLeads = 0 } = drawn.value ?? {};
  const shared = sharedLeads < count ? `, ${describeCount(sharedLeads)} for every country` : "";
  return `needs ${describeCount(needs)}, leads to ${describeCount(count)}${shared}`;
});
const facts = computed(() => {
  if (!drawn.value || isGrid.value) return [];
  const total = drawn.value.total;
  const shown = kind.value === "tree" ? technologies.value : nodes.value.length;
  const count = shown < total ? `${describeCount(shown)} of ${plural(total, "node")}` : plural(total, "node");
  const stops = [
    drawn.value.truncated && "stopped at the node limit",
    nodes.value.some((drawnNode) => drawnNode.deep) && "stopped at the depth limit",
  ].filter(Boolean);
  return [{ label: "Drawn", value: [count, ...stops].join(", ") }];
});

useChip(() =>
  root.value
    ? makeChip(
        labelOf(root.value),
        `The ${root.value.what} ${root.value.name}, selected in PMT's Atlas (${root.value.id})`,
      )
    : null,
);
// The eight subjects opened last are kept for each workspace.
watch(root, (opened) => {
  if (!opened) return;
  const { id: openedId, name, title, what, type, display, picture, silhouette } = opened;
  const entry = { id: openedId, name, title, what, type, display, picture, silhouette };
  recent.value = [entry, ...recent.value.filter((other) => other.id !== openedId)].slice(0, RECENT_KEPT);
});
// The splitter must have laid its panes out before the detail can open or close.
watch([isDetailOpen, splitter], () =>
  nextTick(() => {
    const pane = splitter.value?.panelsRef[DETAIL];
    if (isDetailOpen.value) pane?.expand();
    else pane?.collapse();
  }),
);

/** followPane keeps the toggle in step with the detail pane where the user drags it shut or open. */
function followPane(index: number, size: number, previous: number | undefined): void {
  if (index === DETAIL && previous !== undefined) isDetailOpen.value = size > 0;
}

/** select makes a node the subject, a step of history that clears the rest of the route. */
function select(next: string): void {
  void router.push({ query: { subject: next } });
}

/** focus makes a technology the subject inside its tree, which keeps the view, the direction and the audience. */
function focus(next: string): void {
  void router.push({
    query: {
      subject: next,
      view: view || undefined,
      direction: direction || undefined,
      conditions: conditions || undefined,
    },
  });
}

/** highlight selects a node, or none for "". */
function highlight(next: string): void {
  void router.replace({ query: { ...route.query, node: next || undefined } });
}

/**
 * update changes the filter, and with it whatever else of the route is given. The route keeps only what differs from
 * the defaults.
 */
function update(change: Partial<Filter>, query: Record<string, string> = {}): void {
  const next: Record<string, unknown> = { ...changes.value, ...change };
  for (const [key, value] of Object.entries(next)) {
    if (value === "" || isEqual(value, defaults.value?.[key as keyof Filter])) delete next[key];
  }
  void router.replace({
    query: { ...route.query, ...query, conditions: Object.keys(next).length ? JSON.stringify(next) : undefined },
  });
}

/** toggle opens a fold, a group or a node the depth stopped at, or closes it. A group is selected too. */
function toggle(opening: string): void {
  const expand = opened.value.includes(opening)
    ? opened.value.filter((other) => other !== opening)
    : [...opened.value, opening];
  update({ expand }, isGroup(opening) ? { node: opening } : {});
}

/** pick runs a production method in its group, a step of history. */
function pick(method: string): void {
  const rivals = root.value?.line?.groups?.find((methods) =>
    methods.methods?.some((other) => other.method.id === method),
  );
  const others = (filter.value?.picked ?? []).filter(
    (other) => !rivals?.methods?.some((rival) => rival.method.id === other),
  );
  void router.push({ query: { ...route.query, picked: [...others, method].join(",") } });
}

/** setOrigins draws the checked origins. The last checked origin cannot be unchecked. */
function setOrigins(checked: string[]): void {
  if (checked.length) update({ origins: checked });
}

/** reset puts the filter back to the defaults, and leaves open what was opened. */
function reset(): void {
  const expand = changes.value.expand?.length ? JSON.stringify({ expand: changes.value.expand }) : undefined;
  void router.replace({ query: { ...route.query, conditions: expand } });
}

function turn(next: Record<string, string | undefined>): void {
  void router.replace({ query: { ...route.query, ...next } });
}
</script>

<template>
  <div class="flex h-full min-h-0 bg-default">
    <DefineOrigins>
      <UPopover :content="{ align: 'start' }">
        <UButton
          :label="originsLabel"
          variant="outline"
          size="xs"
          color="neutral"
          :ui="{ label: 'max-w-64 truncate' }"
        />
        <template #content>
          <div class="flex w-80 flex-col gap-2 p-3.5">
            <p class="text-12 text-dimmed">Unchecked origins hide what only they define.</p>
            <UCheckboxGroup
              color="secondary"
              :items="origins"
              :model-value="filter?.origins ?? []"
              :ui="{ ...CHECKBOX_UI, fieldset: 'gap-y-0' }"
              @update:model-value="setOrigins($event as string[])"
            >
              <template #label="{ item }"><OriginChip :origin="item.value" /></template>
            </UCheckboxGroup>
          </div>
        </template>
      </UPopover>
    </DefineOrigins>
    <DefineEnd>
      <template v-if="drawn && !isGrid">
        <UButton
          icon="i-lucide-scan"
          square
          variant="ghost"
          size="xs"
          aria-label="Fit to view"
          @click="canvas?.fit()"
        />
        <UButton
          :icon="isFullscreen ? 'i-lucide-minimize-2' : 'i-lucide-maximize-2'"
          square
          variant="ghost"
          size="xs"
          :aria-label="isFullscreen ? 'Exit full screen' : 'Full screen'"
          @click="toggleFullscreen()"
        />
      </template>
      <UTooltip :text="isDetailOpen ? 'Close the detail' : 'Open the detail of what is selected'">
        <UButton
          icon="i-lucide-panel-right"
          :variant="isDetailOpen ? 'soft' : 'ghost'"
          color="neutral"
          size="xs"
          square
          aria-label="Detail"
          :aria-pressed="isDetailOpen"
          @click="isDetailOpen = !isDetailOpen"
        />
      </UTooltip>
    </DefineEnd>

    <USplitter
      ref="splitter"
      :items="PANES"
      auto-save-id="pmt.atlas"
      class="min-h-0 min-w-0 flex-1"
      @resize="followPane"
    >
      <template #browser>
        <AtlasBrowser :subject="root" @select="select" />
      </template>
      <template #detail="{ collapsed }">
        <AtlasDetail
          v-if="!collapsed"
          :id="detailId"
          :group="group"
          :is-selection="kind === 'tree' || (!!node && node !== root?.id)"
          @ask="select"
          @close="isDetailOpen = false"
        />
      </template>
      <template #view>
        <div ref="stage" class="relative flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden bg-default">
          <InnovationGrid
            v-if="drawn && isGrid"
            class="min-h-0 flex-1"
            :graph="drawn"
            :title="catalogs?.find((catalog) => catalog.types?.includes(Type.TypeTechnology))?.label ?? ''"
            @ask="focus"
          >
            <BarEnd />
          </InnovationGrid>
          <template v-else>
            <div :class="BAR">
              <template v-if="drawn && filter && kind === 'event'">
                <UPopover :content="{ align: 'start' }">
                  <UButton
                    :label="`Show: ${shownDatabases}`"
                    variant="outline"
                    size="xs"
                    color="neutral"
                    :ui="{ label: 'max-w-64 truncate' }"
                  />
                  <template #content>
                    <div class="w-96 p-3.5">
                      <UCheckboxGroup
                        color="secondary"
                        :items="databases"
                        value-key="database"
                        label-key="what"
                        :model-value="filter.databases ?? []"
                        :ui="{ ...CHECKBOX_UI, fieldset: 'grid grid-cols-2 gap-x-5 gap-y-0' }"
                        @update:model-value="update({ databases: $event as string[] })"
                      >
                        <template #label="{ item }">
                          {{ item.what }}<span class="ms-auto text-11 text-dimmed tabular-nums">{{ item.count }}</span>
                        </template>
                      </UCheckboxGroup>
                    </div>
                  </template>
                </UPopover>
                <OriginsMenu />
                <USwitch
                  label="Through scripted effects"
                  color="secondary"
                  size="sm"
                  class="shrink-0"
                  :model-value="filter.follow"
                  @update:model-value="update({ follow: $event })"
                />
                <UPopover :content="{ align: 'start' }">
                  <UButton
                    :label="`Reach: ${filter.depth} deep, ${filter.limit} nodes`"
                    icon="i-lucide-ruler"
                    variant="outline"
                    size="xs"
                    color="neutral"
                  />
                  <template #content>
                    <div class="flex w-80 items-start gap-5 p-3.5">
                      <UFormField label="Depth">
                        <UInputNumber
                          :model-value="filter.depth"
                          v-bind="REACH_DEPTH"
                          orientation="horizontal"
                          size="sm"
                          class="w-24"
                          @update:model-value="$event && update({ depth: $event })"
                        />
                      </UFormField>
                      <UFormField label="Node limit">
                        <UInputNumber
                          :model-value="filter.limit"
                          v-bind="REACH_LIMIT"
                          orientation="horizontal"
                          size="sm"
                          class="w-24"
                          @update:model-value="$event && update({ limit: $event })"
                        />
                      </UFormField>
                    </div>
                    <p class="border-t border-default px-3.5 py-2.5 text-12 text-dimmed">
                      Past {{ filter.fanout }} fires from one node, the rest fold into "N more", which opens.
                    </p>
                  </template>
                </UPopover>
                <UButton v-if="isChanged" label="Reset" variant="ghost" color="error" size="xs" @click="reset" />
              </template>
              <template v-else-if="drawn && filter && kind === 'tree'">
                <h2 class="text-14 font-semibold text-highlighted">{{ tree?.title }}</h2>
                <UFieldGroup size="xs">
                  <UButton
                    label="Whole tree"
                    color="neutral"
                    :variant="view === CHAIN ? 'outline' : 'soft'"
                    @click="turn({ view: undefined })"
                  />
                  <UButton
                    label="Only its chain"
                    color="neutral"
                    :variant="view === CHAIN ? 'soft' : 'outline'"
                    @click="turn({ view: CHAIN })"
                  />
                </UFieldGroup>
                <span v-if="root" class="max-w-96 truncate text-12 text-dimmed">
                  <b class="font-medium text-toned">{{ labelOf(root) }}</b> {{ leads }}
                </span>
                <template v-if="audiences.length">
                  <USelectMenu
                    :model-value="filter.potential"
                    :items="audiences"
                    value-key="value"
                    placeholder="Also show advances of"
                    size="xs"
                    class="w-56"
                    @update:model-value="update({ potential: $event })"
                  >
                    <template #item-trailing="{ item }">
                      <span v-if="'count' in item" class="text-11 text-dimmed tabular-nums">{{ item.count }}</span>
                    </template>
                  </USelectMenu>
                  <UButton
                    v-if="filter.potential"
                    icon="i-lucide-x"
                    color="neutral"
                    variant="ghost"
                    size="xs"
                    square
                    aria-label="Show only the advances of every country"
                    @click="update({ potential: '' })"
                  />
                </template>
              </template>
              <span class="grow" />
              <template v-if="drawn && filter && (kind === 'good' || kind === 'building')">
                <span class="text-12 text-dimmed">Available by</span>
                <USelect
                  :model-value="filter.era || ANY_ERA"
                  :items="eras"
                  size="xs"
                  class="w-44"
                  @update:model-value="update({ era: $event === ANY_ERA ? '' : $event })"
                />
                <OriginsMenu />
              </template>
              <UFieldGroup v-if="drawn && kind === 'tree'" size="xs">
                <UButton
                  icon="i-lucide-arrow-right"
                  color="neutral"
                  :variant="direction === DOWN ? 'outline' : 'soft'"
                  square
                  aria-label="Read left to right"
                  @click="turn({ direction: undefined })"
                />
                <UButton
                  icon="i-lucide-arrow-down"
                  color="neutral"
                  :variant="direction === DOWN ? 'soft' : 'outline'"
                  square
                  aria-label="Read top to bottom"
                  @click="turn({ direction: DOWN })"
                />
              </UFieldGroup>
              <UTooltip v-if="drawn && kind !== 'tree'" text="Fold back every group and every dashed node you opened">
                <UButton
                  label="Collapse all"
                  icon="i-lucide-fold-vertical"
                  variant="ghost"
                  size="xs"
                  :disabled="!opened.length"
                  @click="update({ expand: [] })"
                />
              </UTooltip>
              <BarEnd />
            </div>
            <PaintingBand
              v-if="drawn && kind === 'tree'"
              :painting="tree?.painting"
              class="flex h-10 shrink-0 items-center gap-1 overflow-x-auto border-b border-default bg-muted px-3 whitespace-nowrap"
            >
              <UButton
                v-for="listed in drawn.trees"
                :key="listed.id"
                :variant="listed.id === tree?.id ? 'soft' : 'ghost'"
                color="neutral"
                size="xs"
                :aria-pressed="listed.id === tree?.id"
                @click="focus(listed.first)"
              >
                {{ listed.title }}
                <small class="text-11 font-normal text-dimmed tabular-nums">{{ describeCount(listed.count) }}</small>
              </UButton>
              <span class="ms-auto ps-3 text-12 text-muted tabular-nums">
                {{ describeCount(technologies) }} of {{ describeCount(drawn.total) }} drawn
              </span>
            </PaintingBand>
            <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
            <AtlasStart v-if="!subject" class="min-h-0 flex-1" :recent="recent" @select="select" />
            <AtlasCanvas
              v-else-if="drawn"
              ref="canvas"
              class="min-h-0 min-w-0 flex-1"
              :graph="drawn"
              :kind="kind"
              :selected="node"
              :opened="opened"
              :view="view"
              :direction="direction"
              :full="isFullscreen"
              @select="highlight"
              @ask="kind === 'tree' ? focus($event) : select($event)"
              @open="toggle"
              @pick="pick"
            />
            <p v-else-if="!error" class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed">
              Drawing the canvas
            </p>
          </template>
          <StatusFacts :facts="facts" />
        </div>
      </template>
    </USplitter>
  </div>
</template>
