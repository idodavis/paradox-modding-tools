<script setup lang="ts">
/**
 * The graph's toolbar (the EventGraph board's): the root, searched by any part of an id; Filters; Follow
 * macros; how many nodes are drawn; fit, fold every opened group, full screen, and the detail below or
 * beside. In full screen it floats over the canvas with Exit full screen (the EventGraphFull board's).
 */
import { computed, useTemplateRef } from "vue";
import { useElementSize } from "@vueuse/core";
import type { Roots } from "@services/models";
import { useWorkspaceStore } from "../../stores/workspace";
import { plural } from "../../lib/format";

const { roots, searching, rootKind, nodes, reach, full = false } = defineProps<{
  roots: Roots | null | undefined;
  searching: boolean;
  /** What the root is, for its badge. */
  rootKind: string;
  nodes: number;
  reach: number;
  full?: boolean;
}>();
const emit = defineEmits<{ fit: []; collapse: []; fullscreen: [] }>();
const root = defineModel<string>("root", { required: true });
const searchTerm = defineModel<string>("searchTerm", { required: true });
const follow = defineModel<boolean>("follow", { required: true });
const layout = defineModel<"drawer" | "panel">("layout", { required: true });

const store = useWorkspaceStore();
// the seg's two buttons; a tab's value comes back as a string
const tab = computed({
  get: () => layout.value,
  set: (value: string | number) => (layout.value = value === "panel" ? "panel" : "drawer"),
});
const LAYOUTS = [
  { label: "Detail below", icon: "i-lucide-panel-bottom", value: "panel" },
  { label: "Detail beside", icon: "i-lucide-panel-right", value: "drawer" },
];

// the root's kind sits over the field's end, as wide as its words: a long id stops before it (the badge, the slot's
// 10px end padding and a gap)
const kindBadge = useTemplateRef("kindBadge");
const { width: kindWidth } = useElementSize(kindBadge, undefined, { box: "border-box" });

// the hits under a head per kind, saying how many more the search holds; the root first when the
// search does not hold it. The menu labels every item by id, so a head's id is its text.
const items = computed(() => {
  type Item = { id: string; kind: string; origin?: string; type?: "label" };
  const groups: Item[][] = [];
  for (const count of roots?.databases ?? []) {
    const hits = (roots?.hits ?? []).filter((node) => node.database === count.database);
    if (!hits.length) continue;
    const more = count.count > hits.length ? `, ${count.count - hits.length} more: type to narrow` : "";
    groups.push([{ id: `${hits[0].kind} (${count.count})${more}`, kind: "", type: "label" }, ...hits]);
  }
  if (root.value && !roots?.hits?.some((node) => node.id === root.value)) groups.unshift([{ id: root.value, kind: rootKind }]);
  return groups;
});
</script>

<template>
  <div
    class="flex h-12 shrink-0 items-center gap-2"
    :class="full ? 'rounded-tile bg-elevated px-2 shadow-(--shadow)' : 'border-b border-default bg-muted ps-4 pe-3'"
  >
    <UInputMenu
      v-model="root"
      v-model:search-term="searchTerm"
      :items="items"
      value-key="id"
      label-key="id"
      ignore-filter
      :loading="searching"
      placeholder="An id, a namespace or a number"
      class="shrink-0"
      :class="full ? 'w-75' : 'w-95'"
      :style="{ '--kind': `${kindWidth + 16}px` }"
      :ui="{ base: ['font-mono', full ? 'ps-9' : 'ps-17', rootKind && 'pe-(--kind)'], content: 'min-w-120', itemLabel: 'whitespace-normal break-all' }"
    >
      <template #leading>
        <span class="flex items-center gap-2 text-12 text-muted">
          <UIcon name="i-lucide-target" class="size-4" />
          <span v-if="!full">Root</span>
        </span>
      </template>
      <template v-if="rootKind" #trailing><UBadge ref="kindBadge" :label="rootKind" color="info" size="sm" /></template>
      <template #item-trailing="{ item }">
        <OriginChip v-if="item.origin" :label="store.originName(item.origin)" :color="store.originColor(item.origin)" small />
      </template>
    </UInputMenu>
    <slot name="filters" />
    <UTooltip text="Walk into scripted effects and triggers to find the events they fire">
      <UButton
        label="Follow macros"
        :icon="follow ? 'i-lucide-check' : undefined"
        :variant="follow ? 'soft' : 'outline'"
        :ui="{ leadingIcon: 'text-primary' }"
        @click="follow = !follow"
      />
    </UTooltip>
    <span v-if="!full" class="grow" />
    <USeparator v-else orientation="vertical" class="h-5" />
    <span class="text-12 whitespace-nowrap text-dimmed" title="Drawn, of all the depth reaches: fan-outs past six fold into groups, and the node limit stops the rest">
      {{ reach > nodes ? `${nodes} of ${plural(reach, "node")}` : plural(nodes, "node") }}{{ full ? "" : " shown" }}
    </span>
    <USeparator v-if="!full" orientation="vertical" class="h-5" />
    <UButton icon="i-lucide-scan" square aria-label="Fit to view" @click="emit('fit')" />
    <template v-if="full">
      <UButton label="Exit full screen" icon="i-lucide-minimize-2" @click="emit('fullscreen')">
        <template #trailing><UKbd value="Esc" /></template>
      </UButton>
    </template>
    <template v-else>
      <UTooltip text="Fold every group you opened back into +N">
        <UButton label="Collapse all" icon="i-lucide-fold-vertical" @click="emit('collapse')" />
      </UTooltip>
      <UButton icon="i-lucide-maximize-2" square aria-label="Full screen" @click="emit('fullscreen')" />
      <USeparator orientation="vertical" class="h-5" />
      <UTabs v-model="tab" :items="LAYOUTS" :content="false" :ui="{ label: 'sr-only' }" />
    </template>
  </div>
</template>
