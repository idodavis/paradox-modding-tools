<!--
  The event view: a bar of what the graph shows and how far it reaches, over
  the canvas that draws what fires the subject and what it fires.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef } from "vue";
import { useQuery } from "@pinia/colada";
import { computedAsync, useFullscreen } from "@vueuse/core";
import { omit, union } from "es-toolkit";
import { useRouter } from "vue-router";
import type { Filter } from "@services/graph/models";
import { plural } from "../../lib/format";
import { graphQuery } from "../../queries";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import StatusFacts from "../../ui/StatusFacts.vue";
import GraphCanvas from "./GraphCanvas.vue";
import { buildLayout, FOLD, LOOP_PILL } from "./layout";
import OriginsMenu, { CHECKBOX_UI } from "./OriginsMenu.vue";

const {
  subject,
  defaults,
  conditions = "",
  selected = "",
} = defineProps<{
  subject: string;
  /** The filter of a new event graph. */
  defaults: Filter;
  /** Changes to the defaults, as JSON from the route. */
  conditions?: string;
  /** The id of the selected node, as the route names it; "" is the subject. */
  selected?: string;
}>();
const emit = defineEmits<{ select: [id: string] }>();

const REACH_DEPTH = { min: 1, max: 10 };
const REACH_LIMIT = { min: 10, max: 400, step: 10 };

const router = useRouter();
const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const canvas = useTemplateRef("canvas");
const page = useTemplateRef("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);

/** loops holds the nodes whose loop firers show. */
const loops = shallowRef<string[]>([]);

const edits = computed<Partial<Filter>>(() => (conditions ? JSON.parse(conditions) : {}));
const filter = computed<Filter>(() => ({ ...defaults, ...edits.value }));
const { data: graph, error } = useQuery(() => ({
  ...graphQuery({ subject, filter: filter.value }),
  enabled: workspace.isOpen(),
  placeholderData: (previous) => previous,
}));

const unit = computed(() => Number(settings.value("appearance.scale")) / 100 || 1);
const flow = computedAsync(
  () => graph.value && buildLayout(graph.value, { selected, unit: unit.value, loops: loops.value }),
);
const view = computed(() => `${subject}:${JSON.stringify(omit(edits.value, ["expand"]))}`);

const shownDatabases = computed(() => {
  const [shown, every] = [filter.value.databases ?? [], defaults.databases ?? []];
  if (shown.length === every.length) return "everything";
  if (shown.length === 1) return filter.value.labels?.[shown[0]] ?? shown[0];

  return `${shown.length} of ${every.length}`;
});
const databaseCounts = computed(() =>
  (defaults.databases ?? []).map((database) => ({
    database,
    what: defaults.labels?.[database] ?? database,
    count: graph.value?.nodes?.filter((node) => node.database === database).length ?? 0,
  })),
);
const facts = computed(() => {
  if (!graph.value || !flow.value) return [];
  const drawn = flow.value.nodes.filter((node) => !node.data.loop).length;
  const total = graph.value.total - ((graph.value.nodes?.length ?? 0) - drawn);
  const words = [total > drawn ? `${drawn} of ${plural(total, "node")}` : plural(total, "node")];
  if (graph.value.truncated) words.push("stopped at the node limit");
  if (graph.value.nodes?.some((node) => node.deep)) words.push("stopped at the depth limit");
  return [{ label: "Drawn", value: words.join(", ") }];
});

/**
 * select selects the node with the given id. A fold or a node past the depth
 * opens instead.
 */
function select(nodeId: string): void {
  if (nodeId.startsWith(LOOP_PILL)) {
    loops.value = union(loops.value, [nodeId.slice(LOOP_PILL.length)]);
    return;
  }
  const target = graph.value?.nodes?.find((node) => node.id === nodeId);
  const isFold = nodeId.startsWith(FOLD) || !!target?.count;
  if (!isFold && !target?.deep) {
    emit("select", nodeId);
    return;
  }

  canvas.value?.setAnchor(isFold ? (target?.parent ?? "") : nodeId);
  update({ expand: union(filter.value.expand ?? [], [nodeId]) });
}

function ask(nodeId: string): void {
  void router.push({ query: { subject: nodeId } });
}

function collapseAll(): void {
  loops.value = [];
  update({ expand: [] });
}

/** update changes parts of the filter, which the route keeps as changes. */
function update(change: Partial<Filter>): void {
  const query = { subject, node: selected, conditions: JSON.stringify({ ...edits.value, ...change }) };
  void router.replace({ query: Object.fromEntries(Object.entries(query).filter(([, text]) => text)) });
}

function reset(): void {
  void router.replace({ query: { subject } });
}

/** setNumber sets the depth or the node limit. An emptied field sets none. */
function setNumber(key: "depth" | "limit", count: number | null | undefined): void {
  if (count) update({ [key]: count });
}
</script>

<template>
  <div ref="page" class="relative flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-default">
    <div class="flex h-10 shrink-0 items-center gap-2 overflow-x-auto border-b border-default bg-muted px-3">
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
              :items="databaseCounts"
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
          </div>
        </template>
      </UPopover>
      <OriginsMenu
        :origins="defaults.origins ?? []"
        :model-value="filter.origins ?? []"
        @update:model-value="update({ origins: $event })"
      />
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
                @update:model-value="setNumber('depth', $event)"
              />
            </UFormField>
            <UFormField label="Node limit">
              <UInputNumber
                :model-value="filter.limit"
                v-bind="REACH_LIMIT"
                orientation="horizontal"
                size="sm"
                class="w-24"
                @update:model-value="setNumber('limit', $event)"
              />
            </UFormField>
          </div>
          <p class="border-t border-default px-3.5 py-2.5 text-12 text-dimmed">
            Past {{ filter.fanout }} fires from one node, the rest fold into "N more", which opens.
          </p>
        </template>
      </UPopover>
      <UButton v-if="conditions" label="Reset" variant="ghost" color="error" size="xs" @click="reset" />
      <span class="grow" />
      <UButton
        icon="i-lucide-scan"
        square
        variant="ghost"
        size="xs"
        aria-label="Fit to view"
        :disabled="!flow"
        @click="canvas?.fit()"
      />
      <UTooltip text="Fold back every group and every dashed node you opened">
        <UButton
          label="Collapse all"
          icon="i-lucide-fold-vertical"
          variant="ghost"
          size="xs"
          :disabled="!loops.length && !filter.expand?.length"
          @click="collapseAll"
        />
      </UTooltip>
      <UButton
        :icon="isFullscreen ? 'i-lucide-minimize-2' : 'i-lucide-maximize-2'"
        square
        variant="ghost"
        size="xs"
        :aria-label="isFullscreen ? 'Exit full screen' : 'Full screen'"
        @click="toggleFullscreen()"
      />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <GraphCanvas
      v-if="flow"
      ref="canvas"
      class="min-h-0 min-w-0 flex-1"
      :nodes="flow.nodes"
      :edges="flow.edges"
      :view="view"
      :root="subject"
      :full="isFullscreen"
      @select="select"
      @ask="ask"
    />
    <p v-else-if="!error" class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed">Drawing the graph</p>
    <StatusFacts :facts="facts" />
  </div>
</template>
