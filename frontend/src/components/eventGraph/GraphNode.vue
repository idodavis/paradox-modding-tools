<script setup lang="ts">
/**
 * One node of the graph (pmt.css .gn): an event is a card under its origin's rail, an on_action a filled
 * pill, anything else an outline pill, a stub a dashed group that opens; an event's kind line and a pill's
 * icon take their kind's color. The root is ringed in the accent, the selected node in the link color; off
 * the selected path it fades; zoomed out, it shows its id only.
 */
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";
import type { Node } from "@services/internal/pages/models";
import { kindOf, shapeOf } from "../../composables/useEventGraph";
import { useWorkspaceStore } from "../../stores/workspace";

const {
  node,
  selected = false,
  dim = false,
  lod = false,
} = defineProps<{
  node: Node;
  selected?: boolean;
  dim?: boolean;
  lod?: boolean;
}>();

const store = useWorkspaceStore();
const shape = computed(() => shapeOf(node));
const color = computed(() => store.originColor(node.origin ?? ""));
const kind = computed(() => kindOf(node.kind));
const state = computed(() => [
  node.root && "border-primary ring-3 ring-(--accent-soft)",
  selected && "border-secondary ring-3 ring-(--link-soft)",
  dim && "opacity-40",
]);
</script>

<template>
  <div
    v-if="shape === 'card'"
    class="relative flex flex-col justify-center gap-px overflow-hidden rounded-lg border border-accented bg-elevated whitespace-nowrap"
    :class="[state, lod ? 'h-7 w-37.5 ps-2.75 pe-2' : 'h-14 w-49 ps-3.5 pe-2.5']"
  >
    <i class="absolute inset-y-0 start-0 w-0.75" :style="{ background: color }" />
    <span v-if="!lod" class="flex items-center gap-1.25 text-tiny leading-3.5" :style="{ color: kind.color }">
      <UIcon :name="kind.icon" class="size-3" />{{ node.kind }}
    </span>
    <span class="truncate font-mono font-semibold" :class="lod ? 'text-11' : 'text-12 leading-4.5'">{{ node.id }}</span>
    <span v-if="!lod && node.title" class="truncate text-12 leading-4 text-muted">{{ node.title }}</span>
  </div>
  <div
    v-else-if="shape === 'pill'"
    class="flex items-center overflow-hidden rounded-full border border-accented font-mono whitespace-nowrap"
    :class="[
      state,
      node.kind === 'on action' ? 'bg-muted text-highlighted' : 'bg-default text-muted',
      lod ? 'h-5 w-33 gap-1 ps-1.5 pe-2 text-tiny' : 'h-6.5 w-44 gap-1.5 ps-2 pe-2.5 text-11',
    ]"
  >
    <OriginDot :color="color" small />
    <UIcon :name="kind.icon" class="size-3 shrink-0" :style="{ color: kind.color }" />
    <span class="truncate">{{ node.id }}</span>
  </div>
  <div
    v-else
    class="flex h-6.5 w-44 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed"
    :class="state"
  >
    <UIcon name="i-lucide-layers" class="size-3 shrink-0" />
    <span class="truncate">{{ node.title }}</span>
    <UIcon name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
  <!-- each shape's edges meet at the middle of its sides -->
  <Handle type="target" :position="Position.Left" :connectable="false" />
  <Handle type="source" :position="Position.Right" :connectable="false" />
</template>
