<script setup lang="ts">
/**
 * The graph's controls: its root, searched by any part of an id; how far it reaches; the origins it
 * draws; and whether the detail opens as a drawer or a bottom panel.
 */
import { computed } from "vue";
import type { DatabaseCount, Node } from "@services/models";
import OriginBadge from "../OriginBadge.vue";
import OriginSelectMenu, { type OriginMenuItem } from "../OriginSelectMenu.vue";
import { kindColour } from "../../composables/useEventGraph";
import { originHexByOriginId } from "../../ide/rootDecorations";
import { useWorkspaceStore } from "../../stores/workspace";

const props = defineProps<{
  roots: Node[];
  kinds: DatabaseCount[];
  searching: boolean;
  originItems: OriginMenuItem[];
  truncated: boolean;
  reached: number;
}>();
const emit = defineEmits<{ fit: []; reset: [] }>();
const root = defineModel<string>("root", { required: true });
const searchTerm = defineModel<string>("searchTerm", { required: true });
const rootKind = defineModel<string>("rootKind", { required: true });
const depth = defineModel<number>("depth", { required: true });
const fanout = defineModel<number>("fanout", { required: true });
const maxNodes = defineModel<number>("maxNodes", { required: true });
const origins = defineModel<string[]>("origins", { required: true });
const scripted = defineModel<boolean>("scripted", { required: true });
const layout = defineModel<"drawer" | "panel">("layout", { required: true });

const store = useWorkspaceStore();

// the kind select: every kind the search holds, with its count; a select item cannot be "", so
// every kind is "all" there and "" to the search
const kindItems = computed(() => [
  { label: "All kinds", value: "all" },
  ...props.kinds.map((count) => ({ label: `${count.database} (${count.count})`, value: count.database })),
]);
const kindChoice = computed({
  get: () => rootKind.value || "all",
  set: (value: string) => (rootKind.value = value === "all" ? "" : value),
});

// the hits under a header per kind, and the root first when a new search does not hold it; the
// menu labels every item by id, so a header's id is its text
const items = computed(() => {
  type Item = { id: string; database: string; origin?: string; type?: "label" };
  const groups: Item[][] = [];
  for (const count of props.kinds) {
    const hits = props.roots.filter((node) => node.database === count.database);
    if (!hits.length) continue;
    const more = count.count > hits.length ? ` · ${count.count - hits.length} more, type to narrow` : "";
    const header: Item = { id: `${count.database} (${count.count})${more}`, database: count.database, type: "label" };
    groups.push([header, ...hits]);
  }
  if (root.value && !props.roots.some((node) => node.id === root.value)) groups.unshift([{ id: root.value, database: "" }]);
  return groups;
});
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 border-b border-default px-2 py-1.5 text-xs text-muted">
    <UInputMenu
      v-model="root"
      v-model:search-term="searchTerm"
      :items="items"
      value-key="id"
      label-key="id"
      ignore-filter
      :loading="searching"
      icon="i-lucide-search"
      placeholder="Root: an id, a namespace or a number"
      class="w-80"
      :ui="{ content: 'min-w-[34rem]', itemLabel: 'whitespace-normal break-all' }"
    >
      <template #item-leading="{ item }">
        <span class="h-4 w-1 rounded-full" :style="{ backgroundColor: kindColour(item.database, 80) }" />
      </template>
      <!-- the id already starts with its namespace, so only the origin follows it -->
      <template #item-trailing="{ item }">
        <OriginBadge
          v-if="item.origin"
          :label="store.originName(item.origin)"
          :hex="originHexByOriginId(item.origin)"
        />
      </template>
    </UInputMenu>
    <USelect v-model="kindChoice" :items="kindItems" value-key="value" size="sm" class="w-44" />
    <span>Depth</span>
    <UInputNumber v-model="depth" :min="1" :max="10" size="sm" class="w-24" />
    <span>Fanout</span>
    <UInputNumber v-model="fanout" :min="1" :max="100" size="sm" class="w-24" />
    <span>Nodes</span>
    <UInputNumber v-model="maxNodes" :min="5" :max="400" :step="5" size="sm" class="w-24" />
    <OriginSelectMenu v-model="origins" :items="originItems" />
    <USwitch v-model="scripted" label="Show scripted effects and triggers" size="sm" />
    <UBadge
      v-if="truncated"
      color="warning"
      variant="subtle"
      icon="i-lucide-triangle-alert"
      :label="`Cut at ${maxNodes} nodes at depth ${reached} of ${depth}: raise Nodes to go deeper`"
    />
    <div class="flex-1" />
    <UButton label="Fit" icon="i-lucide-scan" size="sm" color="neutral" variant="ghost" @click="emit('fit')" />
    <UButton label="Reset expands" size="sm" color="neutral" variant="outline" @click="emit('reset')" />
    <UButton
      :icon="layout === 'drawer' ? 'i-lucide-panel-bottom' : 'i-lucide-panel-right'"
      :label="layout === 'drawer' ? 'Detail below' : 'Detail beside'"
      size="sm"
      color="neutral"
      variant="ghost"
      @click="layout = layout === 'drawer' ? 'panel' : 'drawer'"
    />
  </div>
</template>
