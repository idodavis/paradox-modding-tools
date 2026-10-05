<script setup lang="ts">
/**
 * Event Graph: what a root fires and what fires it, drawn around it, with the selected definition
 * opened beside the graph or below it.
 */
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { useVueFlow } from "@vue-flow/core";
import EventGraphToolbar from "../components/eventGraph/EventGraphToolbar.vue";
import GraphCanvas from "../components/eventGraph/GraphCanvas.vue";
import EventDetail from "../components/eventGraph/EventDetail.vue";
import { useEventGraph } from "../composables/useEventGraph";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";

const route = useRoute();
const store = useWorkspaceStore();
const settings = useSettingsStore();
const { id: workspaceId } = defineProps<{ id: string }>();
const live = useLiveEnabled(() => workspaceId);
const { params, selected, searchTerm, rootKind, graph, error, detail, roots, searching, positions, select, setRoot } =
  useEventGraph(() => workspaceId, live);
const { fitView } = useVueFlow("event-graph");

// where the detail opens, kept in the app's settings
const layout = computed({
  get: () => (settings.values["eventGraph.layout"] === "panel" ? "panel" : "drawer"),
  set: (value: "drawer" | "panel") => void settings.set("eventGraph.layout", value),
});

// the origins the graph may draw: the game, then each mod of the playset
const originItems = computed(() =>
  [store.originVanilla, ...store.workspaceMods.map((mod) => mod.id)].map((id) => ({
    id,
    label: store.originName(id),
    color: store.originColor(id),
    thumbnail: store.mod(id)?.thumbnail,
    gameId: id === store.originVanilla ? store.activeWorkspace?.gameId : undefined,
  })),
);

// the IDE's "Open in Event Graph" routes here with ?root=; another workspace starts empty
watch(
  () => route.query.root,
  (root) => typeof root === "string" && root && setRoot(root),
  { immediate: true },
);
watch(() => workspaceId, () => setRoot(""));
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <EventGraphToolbar
      v-model:search-term="searchTerm"
      v-model:root-kind="rootKind"
      v-model:depth="params.depth"
      v-model:fanout="params.fanout"
      v-model:max-nodes="params.maxNodes"
      v-model:origins="params.origins"
      v-model:scripted="params.scripted"
      v-model:layout="layout"
      :root="params.root"
      :roots="roots?.hits ?? []"
      :kinds="roots?.databases ?? []"
      :searching="searching"
      :origin-items="originItems"
      :truncated="!!graph?.truncated"
      :reached="graph?.reached ?? 0"
      @update:root="setRoot"
      @fit="fitView({ padding: 0.2, maxZoom: 1 })"
      @reset="params.expand = []"
    />
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <div class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="relative min-h-0 flex-1">
        <GraphCanvas
          :nodes="graph?.nodes ?? []"
          :edges="graph?.edges ?? []"
          :positions="positions"
          :root-id="params.root"
          :selected-id="selected"
          @select="select"
          @reroot="setRoot"
          @clear="selected = ''"
        />
        <p v-if="graph?.reason" class="pointer-events-none absolute inset-0 grid place-items-center text-sm text-muted">
          {{ graph.reason }}
        </p>
      </div>
      <EventDetail
        :workspace-id="workspaceId"
        :detail="selected ? detail : null"
        :layout="layout"
        @close="selected = ''"
        @root="setRoot"
      />
    </div>
  </div>
</template>
