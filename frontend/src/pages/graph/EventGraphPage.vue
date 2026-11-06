<!-- The Event Graph page: a toolbar, the canvas and the selected node's detail, or a start page without a root. -->
<script setup lang="ts">
import { computed, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useElementSize, useFullscreen, useLocalStorage } from "@vueuse/core";
import { groupBy, union, xor } from "es-toolkit";
import { useRouter } from "vue-router";
import type { Filter, Node } from "@services/graph/models";
import { plural } from "../../lib/format";
import { fileUrl, graphDefaultsQuery, graphQuery, graphRootsQuery, ownRootsQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/toolView";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import EventDetail from "./EventDetail.vue";
import GraphCanvas from "./GraphCanvas.vue";
import { buildLayout, LOOP_PILL, lookupLook } from "./layout";

const {
  id,
  root = "",
  node = "",
  filters = "",
} = defineProps<{
  id: string;
  root?: string;
  /** The selected node, as the route names it. */
  node?: string;
  /** Changes to the default filter, as JSON from the route. */
  filters?: string;
}>();

/** Recent is a root in the recent list: the fields the start page shows. */
type Recent = Pick<Node, "id" | "database" | "name" | "what" | "title" | "display" | "background">;

const NO_FILTER: Filter = { databases: [], origins: [], depth: 0, limit: 0, fanout: 0, expand: [], follow: false };
/** EMPTY is the canvas with nothing drawn. */
const EMPTY = { nodes: [], edges: [] };
/** RECENT is how many roots the recent list keeps. */
const RECENT = 8;
/** START_SHOWN is how many roots each list of the start page shows. */
const START_SHOWN = 4;
/** START_ROW is the classes of a root's row on the start page. */
const START_ROW = "flex h-8.5 w-full items-center gap-2.5 rounded-md px-2 text-start hover:bg-accented";
const LAYOUTS = [
  { label: "Detail below", icon: "i-lucide-panel-bottom", value: "panel" },
  { label: "Detail beside", icon: "i-lucide-panel-right", value: "drawer" },
];
const CHECKBOX_UI = {
  item: "h-6.5 items-center",
  wrapper: "flex grow items-center gap-2",
  label: "flex grow items-center gap-2 text-12",
};

const router = useRouter();
const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const canvas = useTemplateRef("canvas");
const page = useTemplateRef("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);
const recent = useLocalStorage<Recent[]>(() => `pmt.graph.recent.${id}`, []);

const selected = shallowRef(node || root);
/** loops holds the nodes whose loop firers are shown; a new root shows none. */
const loops = shallowRef<string[]>([]);
watch(
  () => root,
  () => (loops.value = []),
);
watch(
  () => root,
  (next) => (selected.value = next),
);
watch(
  () => node,
  (value) => (selected.value = value || selected.value),
);

const history = shallowRef<string[]>([]);
const position = shallowRef(-1);
const backLabel = computed(() => (position.value > 0 ? `Back to ${history.value[position.value - 1]}` : "Back"));
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
const flow = computed(() =>
  graph.value ? buildLayout(graph.value, { selected: selected.value, unit: unit.value, loops: loops.value }) : EMPTY,
);
const kinds = computed(() =>
  (defaults.value?.databases ?? []).map((database) => {
    const nodes = graph.value?.nodes?.filter((entry) => entry.database === database) ?? [];
    return { database, what: defaults.value?.labels?.[database] ?? database, count: nodes.length };
  }),
);
const changes = computed(() => {
  const [now, base] = [filter.value, defaults.value];
  if (!now || !base) return 0;
  const differs = [
    xor(now.databases ?? [], base.databases ?? []).length > 0,
    xor(now.origins ?? [], base.origins ?? []).length > 0,
    now.depth !== base.depth,
    now.limit !== base.limit,
  ];
  return differs.filter(Boolean).length;
});
const origins = computed(() =>
  (defaults.value?.origins ?? []).map((origin) => ({
    value: origin,
    disabled: filter.value?.origins?.length === 1 && filter.value.origins.includes(origin),
  })),
);

const count = computed(() => {
  // a loop's pill folds firers the reach found, so they count as neither drawn nor reached
  const shown = flow.value.nodes.filter((entry) => !entry.data.loop);
  const folded = (graph.value?.nodes?.length ?? 0) - shown.length;
  const drawn = shown.length;
  const total = (graph.value?.total ?? 0) - folded;
  return total > drawn ? `${drawn} of ${plural(total, "node")}` : plural(total, "node");
});
const facts = computed(() => {
  if (!root) return [{ label: "No root", value: "" }];
  if (!graph.value) return [];
  const drawn = [count.value];
  if (graph.value.truncated) drawn.push("stopped at the node limit");
  if (graph.value.nodes?.some((entry) => entry.deep)) drawn.push("the depth limit");
  return [
    { label: "Drawn", value: drawn.join(", ") },
    { label: "Laid out", value: "left to right by what fires what" },
  ];
});

const term = shallowRef("");
const search = refDebounced(term, 200);
// A kept page's menu would stay over another page, so it closes when the page is deactivated.
const menuOpen = shallowRef(false);
onDeactivated(() => (menuOpen.value = false));
const showingOwn = shallowRef(false);
watch(term, () => (showingOwn.value = false));
const { data: roots, isLoading: searching } = useQuery(() => ({
  ...graphRootsQuery(search.value),
  enabled: workspace.isOpen() && !!search.value,
  placeholderData: (previous) => (search.value ? previous : undefined),
}));
const items = computed(() => {
  const head = (label: string) => ({ id: label, name: label, type: "label" as const });
  if (showingOwn.value) return [[head("From your mods"), ...(own.value ?? [])]];
  const groups = Object.entries(groupBy(roots.value ?? [], (entry) => entry.what)).map(([what, nodes]) => [
    head(what),
    ...nodes,
  ]);
  const current = { id: root, name: rootNode.value?.name ?? root };
  return root && !roots.value?.some((entry) => entry.id === root) ? [[current], ...groups] : groups;
});
const rootNode = computed(() => graph.value?.nodes?.find((entry) => entry.id === root));
useChip(() => {
  const selectedNode = graph.value?.nodes?.find((entry) => entry.id === selected.value);
  return selectedNode
    ? makeChip(
        selectedNode.name,
        `The ${selectedNode.what} ${selectedNode.name}, selected in PMT's Event Graph (${selectedNode.id})`,
      )
    : null;
});
const rootLook = computed(() => lookupLook(rootNode.value?.database ?? ""));
// The field's padding leaves room for the badge at its end.
const { width: whatWidth } = useElementSize(useTemplateRef("whatBadge"), undefined, { box: "border-box" });
watch(rootNode, (drawn) => {
  if (!drawn) return;
  const { id: rootId, database, name, what, title, display, background } = drawn;
  const kept = recent.value.filter((entry) => entry.id !== rootId);
  recent.value = [{ id: rootId, database, name, what, title, display, background }, ...kept].slice(0, RECENT);
});

const layout = useLocalStorage<string | number>("pmt.graph.layout", "drawer");
const below = computed(() => layout.value === "panel" && !isFullscreen.value);
const panes = computed(() => {
  const graphPane = { id: "graph", slot: "graph", order: 1, minSize: below.value ? undefined : 50 };
  if (!selected.value || !root) return [graphPane];
  // Sizes are percentages, since a pane added after the splitter exists would read a pixel size as a share.
  const sizes = below.value
    ? { defaultSize: 35, minSize: 20, maxSize: 60 }
    : { defaultSize: 30, minSize: 20, maxSize: 50 };
  return [graphPane, { id: "detail", slot: "detail", order: 2, ...sizes }];
});

/** draw draws the graph from the node with the given id. */
function draw(rootId: string): void {
  update({ expand: [] });
  void router.replace({ query: { root: rootId } });
}

/** step moves through the history by the given offset, back if negative. */
function step(offset: number): void {
  position.value += offset;
  void router.replace({ query: { root: history.value[position.value] } });
}

/** showOwn opens the root's menu on the mods' own events. */
function showOwn(): void {
  showingOwn.value = true;
  menuOpen.value = true;
}

/** select selects the node with the given id. A fold is opened instead of selected. */
function select(nodeId: string): void {
  if (nodeId.startsWith(LOOP_PILL)) {
    loops.value = union(loops.value, [nodeId.slice(LOOP_PILL.length)]);
    return;
  }
  const picked = graph.value?.nodes?.find((candidate) => candidate.id === nodeId);
  const fold = nodeId.startsWith("more:") || !!picked?.count;
  if (!fold) selected.value = nodeId;
  if (fold || picked?.deep) update({ expand: union(filter.value?.expand ?? [], [nodeId]) });
}

/** collapseAll folds back every group, dashed node and loop that was opened. */
function collapseAll(): void {
  loops.value = [];
  update({ expand: [] });
}

/** update changes parts of the filter once Go's defaults have arrived. */
function update(change: Partial<Filter>): void {
  if (filter.value) edits.value = { ...filter.value, ...change };
}

/** setNumber sets the filter's depth or its node limit. An emptied field changes nothing. */
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
      <UTooltip :text="backLabel">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          square
          :aria-label="backLabel"
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
          <OriginChip v-if="'site' in item" :origin="(item as Node).site.place.origin" />
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
      <UButton v-if="isFullscreen" label="Exit full screen" icon="i-lucide-minimize-2" @click="toggleFullscreen()">
        <template #trailing><UKbd value="Esc" /></template>
      </UButton>
      <template v-else>
        <UTooltip text="Fold back every group and every dashed node you opened">
          <UButton
            label="Collapse all"
            icon="i-lucide-fold-vertical"
            :disabled="!root || !graph"
            @click="collapseAll"
          />
        </UTooltip>
        <UButton icon="i-lucide-maximize-2" square aria-label="Full screen" @click="toggleFullscreen()" />
        <USeparator orientation="vertical" class="h-5" />
        <UTabs v-model="layout" :items="LAYOUTS" :content="false" :ui="{ label: 'sr-only' }" />
      </template>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <div v-if="!root" class="flex min-h-0 flex-1 justify-center overflow-auto bg-(--bg-0)">
      <div class="flex w-190 max-w-full flex-col gap-5.5 px-4 pt-24">
        <div class="flex flex-col gap-1.5">
          <span class="text-20 font-semibold text-highlighted">Where to start</span>
          <span class="text-13 text-muted">
            Pick a root to draw what fires it and what it fires. In the editor, <UKbd value="Ctrl" />
            <UKbd value="Shift" /> <UKbd value="G" /> opens the event under the cursor here.
          </span>
        </div>
        <div class="flex items-start gap-7">
          <section v-if="recent.length" class="flex min-w-0 flex-1 flex-col gap-1" aria-label="Recent roots">
            <span class="px-2 pb-1.5 text-11 font-semibold tracking-wide text-dimmed uppercase">Recent</span>
            <button v-for="row in recent.slice(0, START_SHOWN)" :key="row.id" :class="START_ROW" @click="draw(row.id)">
              <span
                v-if="row.display === 'window' && row.background"
                class="h-4 w-5.5 shrink-0 rounded-[3px] bg-cover bg-center"
                :style="{ backgroundImage: `url(&quot;${fileUrl(row.background)}&quot;)` }"
              />
              <UIcon
                v-else
                :name="row.display === 'hidden' ? 'i-lucide-eye-off' : lookupLook(row.database).icon"
                class="size-4 shrink-0"
                :style="{ color: lookupLook(row.database).color }"
              />
              <span class="font-mono text-12 text-highlighted">{{ row.name }}</span>
              <span class="min-w-0 grow truncate text-12 text-dimmed">{{ row.title || row.what }}</span>
            </button>
          </section>
          <section v-if="own?.length" class="flex min-w-0 flex-1 flex-col gap-1" aria-label="Events from your mods">
            <span class="flex items-center px-2 pb-1.5">
              <span class="text-11 font-semibold tracking-wide text-dimmed uppercase">From your mods</span>
              <span class="grow" />
              <UButton :label="`Show all ${own.length}`" variant="link" size="xs" class="p-0" @click="showOwn()" />
            </span>
            <button v-for="row in own.slice(0, START_SHOWN)" :key="row.id" :class="START_ROW" @click="draw(row.id)">
              <OriginChip :origin="row.site.place.origin" dot />
              <span class="font-mono text-12 text-highlighted">{{ row.name }}</span>
              <span class="min-w-0 grow truncate text-12 text-dimmed">{{
                row.display === "hidden" ? "hidden" : row.title || row.what
              }}</span>
              <UBadge
                :label="row.copyOrigin ? 'replaces' : 'added'"
                :color="row.copyOrigin ? 'warning' : 'success'"
                variant="subtle"
                size="sm"
              />
            </button>
          </section>
        </div>
      </div>
    </div>
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
