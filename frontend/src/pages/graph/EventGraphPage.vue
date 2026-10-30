<!--
  The Event Graph page: Back and Forward, the root search and the filters in the toolbar, the canvas, and the detail
  in a side panel or below. With no root it shows where to start. Full screen floats the toolbar over the canvas.
-->
<script setup lang="ts">
import { computed, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { computedAsync, refDebounced, useElementSize, useFullscreen, useLocalStorage } from "@vueuse/core";
import { groupBy, union, xor } from "es-toolkit";
import { useRouter } from "vue-router";
import type { Filter, Node } from "@services/graph/models";
import { plural } from "../../lib/format";
import { graphDefaultsQuery, graphQuery, graphRootsQuery, ownRootsQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/agent";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import EventDetail from "./EventDetail.vue";
import GraphCanvas from "./GraphCanvas.vue";
import GraphStart from "./GraphStart.vue";
import { buildLayout, lookupLook } from "./layout";

const {
  id,
  root = "",
  node = "",
  filters = "",
} = defineProps<{
  id: string;
  root?: string;
  /** The node selected, its path drawn, as the route names it. */
  node?: string;
  /** The filter the graph is drawn with, as JSON, as the route gives it. */
  filters?: string;
}>();

/** A root kept in the recent list, with what the start page shows of it. */
type Recent = Pick<Node, "id" | "database" | "name" | "what" | "title" | "display" | "background">;

const NO_FILTER: Filter = { databases: [], origins: [], depth: 0, limit: 0, fanout: 0, expand: [], follow: false };
/** The canvas with nothing drawn. */
const EMPTY = { nodes: [], edges: [] };
/** How many roots the recent list keeps. */
const RECENT = 8;
const LAYOUTS = [
  { label: "Detail below", icon: "i-lucide-panel-bottom", value: "panel" },
  { label: "Detail beside", icon: "i-lucide-panel-right", value: "drawer" },
];
const BRANCHES = [
  { label: "Path", icon: "i-lucide-split", value: "path" },
  { label: "All", value: "all" },
];
const EXPORTS = [
  { label: "Save as SVG", icon: "i-lucide-file-code", onSelect: () => void canvas.value?.exportSvg() },
  { label: "Save as PNG", icon: "i-lucide-image", onSelect: () => void canvas.value?.exportPng() },
];
const CHECKBOX_UI = {
  item: "h-6.5 items-center",
  wrapper: "flex grow items-center gap-2",
  label: "flex grow items-center gap-2 text-12",
};
/** The words that say how the chain is laid out, for each choice of the nodes that open. */
const LAID_OUT: Record<string, string> = {
  path: "left to right by step, the path's branches open",
  all: "left to right by step, every node that fires open",
};

const router = useRouter();
const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const canvas = useTemplateRef("canvas");
const page = useTemplateRef("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);
const branches = useLocalStorage("pmt.graph.branches", "path");
const recent = useLocalStorage<Recent[]>(() => `pmt.graph.recent.${id}`, []);

const selected = shallowRef(node || root);
watch(
  () => root,
  (next) => (selected.value = next),
);
watch(
  () => node,
  (value) => (selected.value = value || selected.value),
);

// the roots drawn on this page, and where Back and Forward stand among them
const history = shallowRef<string[]>([]);
const position = shallowRef(-1);
watch(
  () => root,
  (next) => {
    if (!next || history.value[position.value] === next) return;
    history.value = [...history.value.slice(0, position.value + 1), next];
    position.value = history.value.length - 1;
  },
  { immediate: true },
);

const { data: defaults } = useQuery(() => ({ ...graphDefaultsQuery, enabled: workspace.isOpen() }));
// the filter is Go's defaults, with the parts the user or the route changed laid over them
const edits = shallowRef<Partial<Filter> | null>(null);
watch(
  () => id,
  () => (edits.value = null),
);
watch(
  () => filters,
  (json) => (edits.value = json ? (JSON.parse(json) as Partial<Filter>) : edits.value),
  { immediate: true },
);
const filter = computed(() => defaults.value && { ...defaults.value, ...edits.value });
const { data: graph, error } = useQuery(() => ({
  ...graphQuery({ root, filter: filter.value ?? NO_FILTER }),
  enabled: workspace.isOpen() && !!root && !!filter.value,
  placeholderData: (previous) => (root ? previous : undefined),
}));
const { data: own } = useQuery(() => ({ ...ownRootsQuery, enabled: workspace.isOpen() }));
const unit = computed(() => Number(settings.value("appearance.scale")) / 100 || 1);
// ELK lays the graph out in a worker; the canvas keeps the last layout until the next one arrives
const flow = computedAsync(() => {
  if (!graph.value) return EMPTY;
  const options = { branches: branches.value, selected: selected.value, unit: unit.value };
  return buildLayout(graph.value, {
    ...options,
    fanout: filter.value?.fanout ?? 0,
    expand: filter.value?.expand ?? [],
  });
}, EMPTY);
const kinds = computed(() =>
  (defaults.value?.databases ?? []).map((database) => {
    const nodes = graph.value?.nodes?.filter((one) => one.database === database) ?? [];
    return { database, what: defaults.value?.labels?.[database] ?? database, count: nodes.length };
  }),
);
const changes = computed(() => {
  const [now, base] = [filter.value, defaults.value];
  if (!now || !base) return 0;
  const differences = [
    xor(now.databases ?? [], base.databases ?? []).length,
    xor(now.origins ?? [], base.origins ?? []).length,
    now.depth - base.depth,
    now.limit - base.limit,
  ];
  return differences.filter(Boolean).length;
});
const origins = computed(() =>
  (defaults.value?.origins ?? []).map((origin) => ({
    value: origin,
    disabled: filter.value?.origins?.length === 1 && filter.value.origins.includes(origin),
  })),
);

const count = computed(() => {
  const drawn = graph.value?.nodes?.length ?? 0;
  const total = graph.value?.total ?? 0;
  return total > drawn ? `${drawn} of ${plural(total, "node")}` : plural(total, "node");
});
const facts = computed(() =>
  graph.value
    ? [
        {
          label: "Drawn",
          value: `${count.value}${graph.value.truncated ? ", stopped at the node limit" : ""}${graph.value.nodes?.some((one) => one.deep) ? ", the depth limit" : ""}`,
        },
        { label: "Laid out", value: LAID_OUT[branches.value] ?? "" },
      ]
    : [{ label: "No root", value: "pick one to draw" }],
);

const term = shallowRef("");
const search = refDebounced(term, 200);
// the root's menu closes when another page is shown, since a kept page's menu would stay over it
const menuOpen = shallowRef(false);
onDeactivated(() => (menuOpen.value = false));
// the menu lists the mods' own events, as the start page's Show all asks, until the user types
const showingOwn = shallowRef(false);
watch(term, () => (showingOwn.value = false));
const { data: roots, isLoading: searching } = useQuery(() => ({
  ...graphRootsQuery(search.value),
  enabled: workspace.isOpen() && !!search.value,
  placeholderData: (previous) => (search.value ? previous : undefined),
}));
// the hits under a head per kind, the root first when the search does not hold it
const items = computed(() => {
  const head = (label: string) => ({ id: label, name: label, type: "label" as const });
  if (showingOwn.value) return [[head("From your mods"), ...(own.value ?? [])]];
  const heads = Object.entries(groupBy(roots.value ?? [], (one) => one.what)).map(([what, nodes]) => [
    head(what),
    ...nodes,
  ]);
  const shown = { id: root, name: rootNode.value?.name ?? root };
  return root && !roots.value?.some((one) => one.id === root) ? [[shown], ...heads] : heads;
});
const rootNode = computed(() => graph.value?.nodes?.find((one) => one.id === root));
// the node selected goes with an agent's prompt
useChip(() => {
  const selectedNode = graph.value?.nodes?.find((one) => one.id === selected.value);
  return selectedNode
    ? makeChip(
        selectedNode.name,
        `The ${selectedNode.what} ${selectedNode.name}, selected in PMT's Event Graph (${selectedNode.id})`,
      )
    : null;
});
const rootLook = computed(() => lookupLook(rootNode.value?.database ?? ""));
// the root's kind sits over the field's end, as wide as its words: a long id stops before it
const { width: whatWidth } = useElementSize(useTemplateRef("whatBadge"), undefined, { box: "border-box" });
// the root drawn joins the recent list, newest first
watch(rootNode, (drawn) => {
  if (!drawn) return;
  const { id: rootId, database, name, what, title, display, background } = drawn;
  const kept = recent.value.filter((one) => one.id !== rootId);
  recent.value = [{ id: rootId, database, name, what, title, display, background }, ...kept].slice(0, RECENT);
});

// the detail opens beside the graph or below it; full screen keeps it beside
const layout = useLocalStorage<string | number>("pmt.graph.layout", "drawer");
const below = computed(() => layout.value === "panel" && !isFullscreen.value);
const panes = computed(() => [
  { id: "graph", slot: "graph", order: 1, minSize: below.value ? undefined : 50 },
  ...(selected.value && root
    ? [
        // in percent: a pane in pixels that appears after the splitter is made is taken as a share
        {
          id: "detail",
          slot: "detail",
          order: 2,
          ...(below.value
            ? { defaultSize: 35, minSize: 20, maxSize: 60 }
            : { defaultSize: 30, minSize: 20, maxSize: 50 }),
        },
      ]
    : []),
]);

/** Draws the graph from the node with the given id. */
function draw(next: string): void {
  update({ expand: [] });
  void router.replace({ query: { root: next } });
}

/** Goes back to the root drawn before this one, or forward again for a positive step. */
function step(by: number): void {
  position.value += by;
  void router.replace({ query: { root: history.value[position.value] } });
}

/** Opens the root's menu on the mods' own events. */
function showOwn(): void {
  showingOwn.value = true;
  menuOpen.value = true;
}

/** Selects the node with the given id. A fold is opened instead, and a node the depth stopped at is opened as well. */
function select(nodeId: string): void {
  const picked = graph.value?.nodes?.find((candidate) => candidate.id === nodeId);
  const fold = nodeId.startsWith("more:") || !!picked?.count;
  if (!fold) selected.value = nodeId;
  if (fold || picked?.deep) update({ expand: union(filter.value?.expand ?? [], [nodeId]) });
}

/** Changes parts of the filter. Nothing changes until Go's defaults have arrived. */
function update(change: Partial<Filter>): void {
  if (filter.value) edits.value = { ...filter.value, ...change };
}

/** Sets the filter's depth or its node limit. An emptied field changes nothing. */
function setNumber(key: "depth" | "limit", value: number | null | undefined): void {
  if (value) update({ [key]: value });
}
</script>

<template>
  <div ref="page" class="relative flex h-full min-h-0 flex-col overflow-hidden bg-default">
    <div
      class="flex h-12 shrink-0 items-center gap-2"
      :class="
        isFullscreen
          ? 'absolute top-3 left-4 z-10 rounded-tile bg-elevated px-2 shadow-(--shadow)'
          : 'border-b border-default bg-muted ps-3 pe-3'
      "
    >
      <UTooltip :text="position > 0 ? `Back to ${history[position - 1]}` : 'Back'">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          square
          :aria-label="position > 0 ? `Back to ${history[position - 1]}` : 'Back'"
          :disabled="position <= 0"
          @click="step(-1)"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-arrow-right"
        variant="ghost"
        square
        aria-label="Forward"
        :disabled="position >= history.length - 1"
        @click="step(1)"
      />
      <UInputMenu
        v-model:search-term="term"
        v-model:open="menuOpen"
        :model-value="root"
        :items="items"
        ignore-filter
        :loading="searching"
        :content="{ align: 'start' }"
        placeholder="An id, a namespace or a number"
        class="w-75 shrink-0"
        value-key="id"
        label-key="name"
        :style="{ '--what': `${whatWidth + 16}px` }"
        :ui="{
          base: [
            'font-mono placeholder:font-sans placeholder:text-13',
            isFullscreen ? 'ps-9' : 'ps-17',
            rootNode && 'pe-(--what)',
          ],
          content: 'min-w-140',
          itemLabel: 'truncate',
        }"
        @update:model-value="(next) => next && draw(next)"
      >
        <template #leading>
          <span class="flex items-center gap-2 text-12 text-muted">
            <UIcon name="i-lucide-target" class="size-4" />
            <span v-if="!isFullscreen">Root</span>
          </span>
        </template>
        <template v-if="rootNode" #trailing>
          <UBadge
            ref="whatBadge"
            :label="`${rootNode.display === 'hidden' ? 'hidden ' : ''}${rootNode.what}`"
            :icon="rootNode.display === 'hidden' ? 'i-lucide-eye-off' : rootLook.icon"
            size="sm"
            class="bg-current/16"
            :style="{ color: rootLook.color }"
          />
        </template>
        <template #item-label="{ item }">
          <span class="font-mono">{{ item.name }}</span>
          <span v-if="'title' in item && item.title" class="ms-2 text-dimmed">{{ item.title }}</span>
        </template>
        <template #item-trailing="{ item }">
          <OriginChip v-if="'place' in item" :origin="(item as Node).place.origin" />
        </template>
      </UInputMenu>
      <UPopover :content="{ align: 'start' }">
        <UButton label="Filters" icon="i-lucide-filter" size="md">
          <template v-if="changes" #trailing><UBadge :label="String(changes)" size="sm" /></template>
        </UButton>
        <template #content>
          <div v-if="filter" class="w-110">
            <section class="flex flex-col gap-2 border-b border-default px-3.5 py-3">
              <div class="flex items-baseline gap-2">
                <span class="text-12 font-semibold">Kinds</span>
                <span class="text-12 text-dimmed">Which kinds of object become nodes</span>
              </div>
              <UCheckboxGroup
                :items="kinds"
                value-key="database"
                label-key="what"
                :model-value="filter.databases ?? []"
                :ui="{ ...CHECKBOX_UI, fieldset: 'grid grid-cols-2 gap-x-5 gap-y-0' }"
                @update:model-value="update({ databases: $event })"
              >
                <template #label="{ item }">
                  {{ item.what }}<span class="ms-auto text-11 text-dimmed tabular-nums">{{ item.count }}</span>
                </template>
              </UCheckboxGroup>
            </section>
            <section class="flex flex-col gap-2 border-b border-default px-3.5 py-3">
              <span class="text-12 font-semibold">Show nodes from</span>
              <p class="text-12 text-dimmed">Unchecked origins hide the nodes only they define.</p>
              <UCheckboxGroup
                :items="origins"
                :model-value="filter.origins ?? []"
                :ui="{ ...CHECKBOX_UI, fieldset: 'gap-y-0' }"
                @update:model-value="update({ origins: $event })"
              >
                <template #label="{ item }"><OriginChip :origin="item.value" /></template>
              </UCheckboxGroup>
            </section>
            <section class="flex items-start gap-5 border-b border-default px-3.5 py-3">
              <UFormField label="Depth">
                <UInputNumber
                  :model-value="filter.depth"
                  :min="1"
                  :max="10"
                  orientation="horizontal"
                  size="sm"
                  class="w-24"
                  @update:model-value="setNumber('depth', $event)"
                />
              </UFormField>
              <UFormField label="Node limit">
                <UInputNumber
                  :model-value="filter.limit"
                  :min="10"
                  :max="400"
                  :step="10"
                  orientation="horizontal"
                  size="sm"
                  class="w-24"
                  @update:model-value="setNumber('limit', $event)"
                />
              </UFormField>
              <p class="pt-5.5 text-12 text-dimmed">
                Past {{ filter.fanout }} fires from one node, the rest fold into "N more", which opens.
              </p>
            </section>
            <div class="flex items-center gap-2 bg-muted px-3.5 py-2.5">
              <UButton label="Reset to defaults" variant="ghost" @click="edits = null" />
              <span class="grow" />
              <span class="text-12 text-dimmed">Changes apply as you pick</span>
            </div>
          </div>
        </template>
      </UPopover>
      <UTooltip text="Walk into scripted effects to find the events they fire">
        <UButton
          label="Follow macros"
          :icon="filter?.follow ? 'i-lucide-check' : undefined"
          :variant="filter?.follow ? 'soft' : 'outline'"
          :ui="{ leadingIcon: 'text-primary' }"
          @click="update({ follow: !filter?.follow })"
        />
      </UTooltip>
      <UTooltip text="Which nodes open into their branches: those on the path, or all">
        <UTabs
          :model-value="branches"
          :items="BRANCHES"
          :content="false"
          size="sm"
          @update:model-value="branches = String($event)"
        />
      </UTooltip>
      <span v-if="!isFullscreen" class="grow" />
      <USeparator v-else orientation="vertical" class="h-5" />
      <span
        v-if="graph"
        class="text-12 whitespace-nowrap text-dimmed"
        title="Drawn, of all the depth reaches: the node limit stops the rest"
      >
        {{ count }}{{ isFullscreen ? "" : " shown" }}
      </span>
      <USeparator v-if="!isFullscreen" orientation="vertical" class="h-5" />
      <UButton icon="i-lucide-scan" square aria-label="Fit to view" :disabled="!graph" @click="canvas?.fit()" />
      <UDropdownMenu :items="EXPORTS" :content="{ align: 'end' }">
        <UButton label="Export" icon="i-lucide-download" trailing-icon="i-lucide-chevron-down" :disabled="!graph" />
      </UDropdownMenu>
      <UButton v-if="isFullscreen" label="Exit full screen" icon="i-lucide-minimize-2" @click="toggleFullscreen()">
        <template #trailing><UKbd value="Esc" /></template>
      </UButton>
      <template v-else>
        <UTooltip text="Fold back every group and every dashed node you opened">
          <UButton
            label="Collapse all"
            icon="i-lucide-fold-vertical"
            :disabled="!graph"
            @click="update({ expand: [] })"
          />
        </UTooltip>
        <UButton icon="i-lucide-maximize-2" square aria-label="Full screen" @click="toggleFullscreen()" />
        <USeparator orientation="vertical" class="h-5" />
        <UTabs v-model="layout" :items="LAYOUTS" :content="false" :ui="{ label: 'sr-only' }" />
      </template>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <GraphStart
      v-if="!root"
      :recent="recent as Node[]"
      :own="own ?? []"
      class="min-h-0 flex-1"
      @draw="draw"
      @show-all="showOwn"
    />
    <USplitter
      v-else
      :key="below ? 'below' : 'beside'"
      :items="panes"
      :orientation="below ? 'vertical' : 'horizontal'"
      :auto-save-id="below ? 'pmt.graph.panel' : 'pmt.graph.drawer'"
      class="min-h-0 flex-1"
    >
      <template #graph>
        <GraphCanvas
          v-if="graph"
          ref="canvas"
          :nodes="flow.nodes"
          :edges="flow.edges"
          :view="root"
          :root="root"
          :full="isFullscreen"
          :labels="filter?.labels ?? {}"
          @select="select"
          @draw="draw"
        />
        <p v-else-if="!error" class="grid h-full min-w-0 flex-1 place-items-center text-13 text-dimmed">
          Drawing the graph
        </p>
      </template>
      <template #detail>
        <EventDetail
          :id="selected"
          :graph="graph"
          :below="below"
          class="h-full border-default"
          :class="below ? 'border-t' : 'border-s'"
          @close="selected = ''"
          @select="select"
        />
      </template>
    </USplitter>
    <StatusFacts :facts="facts" />
  </div>
</template>
