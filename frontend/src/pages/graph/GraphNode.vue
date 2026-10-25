<!-- One node of the graph, drawn as a card or a pill and colored and iconed by its database. -->
<script setup lang="ts">
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";
import type { Node } from "@services/graph/models";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import { lookupLook } from "./layout";

const { node } = defineProps<{ node: Node }>();

/** The classes that fade a node off the lit route and ring the root and the selected node. */
const STATE =
  "in-[.dim]:opacity-40 in-[.root]:border-primary in-[.root]:ring-3 in-[.root]:ring-(--accent-soft) in-[.selected]:border-secondary in-[.selected]:ring-3 in-[.selected]:ring-(--link-soft)";

const workspace = useWorkspaceStore();
const look = computed(() => lookupLook(node.database));
</script>

<template>
  <div
    v-if="node.count"
    :class="STATE"
    class="flex h-6.5 w-44 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed"
  >
    <UIcon name="i-lucide-layers" class="size-3 shrink-0" />
    <span class="truncate">{{ node.count }} more</span>
    <UIcon name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
  <div
    v-else-if="look.shape === 'card'"
    :class="STATE"
    class="relative flex h-14 w-49 flex-col justify-center gap-px overflow-hidden rounded-lg border border-accented bg-elevated ps-3.5 pe-2.5 whitespace-nowrap"
  >
    <i class="absolute inset-y-0 start-0 w-0.75" :style="{ background: workspace.originColor(node.place.origin) }" />
    <span
      class="flex items-center gap-1.25 text-tiny leading-3.5 in-[.zoomed-out]:hidden"
      :style="{ color: look.color }"
    >
      <UIcon :name="look.icon" class="size-3" />{{ node.what }}
    </span>
    <span class="truncate font-mono text-12 leading-4.5 font-semibold">{{ node.name }}</span>
    <span v-if="node.title" class="truncate text-12 leading-4 text-muted in-[.zoomed-out]:hidden">{{
      node.title
    }}</span>
  </div>
  <div
    v-else
    :class="[STATE, look.filled ? 'bg-muted text-highlighted' : 'bg-default text-muted']"
    class="flex h-6.5 w-44 items-center gap-1.5 overflow-hidden rounded-full border border-accented ps-2 pe-2.5 font-mono text-11 whitespace-nowrap"
  >
    <OriginChip :origin="node.place.origin" dot />
    <UIcon :name="look.icon" class="size-3 shrink-0" :style="{ color: look.color }" />
    <span class="truncate">{{ node.name }}</span>
  </div>
  <Handle type="target" :position="Position.Left" :connectable="false" />
  <Handle type="source" :position="Position.Right" :connectable="false" />
</template>
