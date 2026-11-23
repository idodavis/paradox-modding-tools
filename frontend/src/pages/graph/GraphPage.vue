<!--
  The Graph page: a browser of the game's definitions beside the view of the
  subject, or the start page where there is none, and the detail of what is
  selected in a pane that starts closed.
-->
<script setup lang="ts">
import { computed, watch } from "vue";
import type { SplitterItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { useRouter } from "vue-router";
import { Type } from "@internal/lang/models";
import type { Filter } from "@services/graph/models";
import { detailQuery, graphDefaultsQuery, graphLineQuery, graphTreeQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/toolView";
import { useWorkspaceStore } from "../../stores/workspace";
import BuildingMethods from "./BuildingMethods.vue";
import BuildingSheet from "./BuildingSheet.vue";
import BuildingTrack from "./BuildingTrack.vue";
import DetailPane from "./DetailPane.vue";
import EventGraph from "./EventGraph.vue";
import GoodSheet from "./GoodSheet.vue";
import GraphBrowser from "./GraphBrowser.vue";
import GraphStart from "./GraphStart.vue";
import InnovationGrid from "./InnovationGrid.vue";
import { GOOD_TYPES, labelOf, LINE_TYPES, NO_REACH, type Recent } from "./nodes";
import TechTree, { type TechChange } from "./TechTree.vue";

const {
  id,
  subject = "",
  node = "",
  conditions = "",
  picked = [],
  view = "",
  direction = "",
  wires = "",
} = defineProps<{
  id: string;
  /** The node the page is about, as the route names it. */
  subject?: string;
  /** The node selected in the subject's view, as the route names it. */
  node?: string;
  /** Changes to the view's filter, as JSON from the route. */
  conditions?: string;
  /** The ids of the production methods picked, as the route keeps them. */
  picked?: string | string[];
  /** The view of a technology's tree: "chain" for its chain alone. */
  view?: string;
  /** The way a tree reads: "down" for top to bottom. */
  direction?: string;
  /** What a tree does with the wires outside the chain: "hidden". */
  wires?: string;
}>();

/** RECENT_KEPT is how many subjects the start page remembers. */
const RECENT_KEPT = 8;
const BROWSER: SplitterItem = {
  id: "browser",
  slot: "browser",
  order: 1,
  defaultSize: 16,
  minSize: 12,
  maxSize: 35,
  collapsible: true,
  collapsedSize: 0,
};
const VIEW: SplitterItem = { id: "view", slot: "view", order: 2, minSize: 30 };
const DETAIL: SplitterItem = { id: "detail", slot: "detail", order: 3, defaultSize: 24, minSize: 16, maxSize: 45 };

const router = useRouter();
const workspace = useWorkspaceStore();
const recent = useLocalStorage<Recent[]>(() => `pmt.graph.recent.${id}`, []);
const isDetailOpen = useLocalStorage("pmt.graph.detail", false);
const panes = computed(() => (isDetailOpen.value ? [BROWSER, VIEW, DETAIL] : [BROWSER, VIEW]));

const { data: defaults } = useQuery(() => ({ ...graphDefaultsQuery, enabled: workspace.isOpen() }));
const { data: detail, error } = useQuery(() => ({
  ...detailQuery(subject),
  enabled: workspace.isOpen() && !!subject,
}));
const subjectNode = computed(() => detail.value?.node);
const isEvent = computed(() => !!defaults.value?.databases?.includes(subject.slice(0, subject.indexOf(":"))));
const isGood = computed(() => GOOD_TYPES.includes(subjectNode.value?.type));
const isLine = computed(() => LINE_TYPES.includes(subjectNode.value?.type));
const isTechnology = computed(() => subjectNode.value?.type === Type.TypeTechnology);
const pickedIds = computed(() => [picked].flat());
const edits = computed<Partial<Filter>>(() => (conditions ? JSON.parse(conditions) : {}));
const { data: line, error: lineError } = useQuery(() => ({
  ...graphLineQuery({ id: subject, picked: pickedIds.value }),
  enabled: workspace.isOpen() && isLine.value,
  placeholderData: (previous) => previous,
}));
const { data: tree, error: treeError } = useQuery(() => ({
  ...graphTreeQuery({ id: subject, filter: { ...NO_REACH, potential: edits.value.potential } }),
  enabled: workspace.isOpen() && isTechnology.value,
  placeholderData: (previous) => previous,
}));

useChip(() =>
  subjectNode.value
    ? makeChip(
        subjectNode.value.name,
        `The ${subjectNode.value.what} ${subjectNode.value.name}, selected in PMT's Graph (${subjectNode.value.id})`,
      )
    : null,
);
watch(subjectNode, (node) => {
  if (!node) return;
  const { id: nodeId, database, name, what, title, display, picture, silhouette, type } = node;
  const latest = { id: nodeId, database, name, what, title, display, picture, silhouette, type };
  recent.value = [latest, ...recent.value.filter((earlier) => earlier.id !== nodeId)].slice(0, RECENT_KEPT);
});

/** select makes the node with the given id the subject, a step of history. */
function select(nodeId: string): void {
  void router.push({ query: { subject: nodeId } });
}

/**
 * routeQuery returns the route's query with the given changes. A part left
 * empty is left out.
 */
function routeQuery(change: Record<string, string | string[]>): Record<string, string | string[]> {
  const query = { subject, node, conditions, picked: pickedIds.value, view, direction, wires, ...change };

  return Object.fromEntries(Object.entries(query).filter(([, part]) => part.length));
}

/** highlight selects a node in the subject's view, replacing the route. */
function highlight(nodeId: string): void {
  void router.replace({ query: routeQuery({ node: nodeId }) });
}

/** pick makes the given production methods the ones the groups run. */
function pick(ids: string[]): void {
  void router.push({ query: routeQuery({ picked: ids }) });
}

/**
 * focus makes a technology the subject and keeps the view, the direction and
 * the audience, so a click inside a tree stays in it.
 */
function focus(technologyId: string): void {
  void router.push({ query: routeQuery({ subject: technologyId, node: "", picked: [] }) });
}

/**
 * change replaces the route with a change a technology's views ask for. An
 * audience is a change to the filter, which the route keeps as JSON.
 */
function change({ potential, ...parts }: TechChange): void {
  const filter =
    potential === undefined ? conditions : JSON.stringify({ ...edits.value, potential: potential || undefined });
  void router.replace({ query: routeQuery({ ...parts, conditions: filter }) });
}
</script>

<template>
  <div class="flex h-full min-h-0 bg-default">
    <USplitter :items="panes" auto-save-id="pmt.graph" class="min-h-0 min-w-0 flex-1">
      <template #browser>
        <GraphBrowser :subject="(isLine && line?.head) || subjectNode" @select="select" />
      </template>
      <template #detail>
        <DetailPane :id="node || subject" @ask="select" @close="isDetailOpen = false" />
      </template>
      <template #view>
        <div class="flex h-full min-h-0 w-full min-w-0 flex-col">
          <UAlert
            v-if="error || lineError || treeError"
            color="error"
            variant="subtle"
            :description="(error ?? lineError ?? treeError)?.message"
            class="m-2"
          />
          <GraphStart v-if="!subject" class="min-h-0 flex-1" :recent="recent" @select="select" />
          <EventGraph
            v-else-if="defaults && isEvent"
            :key="subject"
            class="min-h-0 flex-1"
            :subject="subject"
            :defaults="defaults"
            :conditions="conditions"
            :selected="node"
            @select="highlight"
          />
          <GoodSheet
            v-else-if="defaults && isGood"
            :key="subject"
            class="min-h-0 flex-1"
            :subject="subject"
            :defaults="defaults"
            :conditions="conditions"
            :selected="node"
            @ask="select"
          />
          <BuildingMethods
            v-else-if="isLine && line?.totals"
            :key="subject"
            class="min-h-0 flex-1"
            :line="line"
            @pick="pick"
            @ask="select"
          />
          <BuildingSheet
            v-else-if="isLine && line?.levels?.length === 1"
            :key="subject"
            class="min-h-0 flex-1"
            :line="line"
            :selected="node"
            @select="highlight"
            @ask="select"
          />
          <BuildingTrack
            v-else-if="isLine && line"
            :key="subject"
            class="min-h-0 flex-1"
            :line="line"
            :selected="node || subject"
            @select="highlight"
            @ask="select"
          />
          <p v-else-if="isLine && !lineError" class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed">
            Drawing the line
          </p>
          <InnovationGrid
            v-else-if="isTechnology && tree && !tree.requires?.length"
            :key="tree.tree"
            class="min-h-0 flex-1"
            :tree="tree"
            @ask="focus"
          />
          <TechTree
            v-else-if="isTechnology && tree"
            class="min-h-0 flex-1"
            :tree="tree"
            :view="view"
            :direction="direction"
            :wires="wires"
            :potential="edits.potential"
            @ask="focus"
            @change="change"
          />
          <p v-else-if="isTechnology && !treeError" class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed">
            Drawing the tree
          </p>
          <p
            v-else-if="defaults && subjectNode && !isLine && !isTechnology"
            class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed"
          >
            {{ labelOf(subjectNode) }} ({{ subjectNode.what }}): its view is not built yet.
          </p>
        </div>
      </template>
    </USplitter>
    <div class="flex w-8 shrink-0 flex-col items-center border-s border-default bg-muted pt-1.5">
      <UTooltip :text="isDetailOpen ? 'Close the detail' : 'Open the detail of what is selected'">
        <UButton
          icon="i-lucide-panel-right"
          :variant="isDetailOpen ? 'soft' : 'ghost'"
          color="neutral"
          size="sm"
          square
          aria-label="Detail"
          @click="isDetailOpen = !isDetailOpen"
        />
      </UTooltip>
    </div>
  </div>
</template>
