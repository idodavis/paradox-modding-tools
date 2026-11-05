<!--
  One item of the canvas: an event's frame, a message's toast, a pill for any other entry, a fold's pill, or a loop's
  pill: the firers that come round a loop, or a firing back into its own chain.
-->
<script setup lang="ts">
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";
import { fileUrl } from "../../queries";
import OriginChip from "../../ui/OriginChip.vue";
import type { Drawn } from "./layout";

const { drawn } = defineProps<{ drawn: Drawn }>();

/** STATE is the classes that fade a node off the lit route and ring the root and the selection. */
const STATE =
  "in-[.dim]:opacity-40 in-[.root]:border-primary in-[.root]:ring-3 in-[.root]:ring-(--accent-soft) in-[.selected]:border-secondary in-[.selected]:ring-3 in-[.selected]:ring-(--link-soft)";
/** HATCHED is the hatching of a hidden event's scene. */
const HATCHED = "bg-[repeating-linear-gradient(135deg,var(--bg-2)_0_6px,var(--bg-1)_6px_12px)]";

const node = computed(() => drawn.node);
const hidden = computed(() => node.value.display === "hidden");
/** dashed is true for a node beyond the depth limit that is not the root. */
const dashed = computed(() => node.value.deep && node.value.step !== 0);
/** fill is the colors of a pill. */
const fill = computed(() => (drawn.look.filled ? "bg-muted text-highlighted" : "bg-default text-muted"));
const icon = computed(() => (hidden.value ? "i-lucide-eye-off" : drawn.look.icon));
const scene = computed(() =>
  node.value.background ? { backgroundImage: `url("${fileUrl(node.value.background)}")` } : undefined,
);
/** The frame's first line: the title, else what it is. */
const caption = computed(() => {
  if (dashed.value) return "Beyond depth, expand to load";
  return node.value.title || (hidden.value ? "hidden event" : node.value.what);
});
</script>

<template>
  <div
    v-if="drawn.loop"
    :class="STATE"
    :title="
      drawn.loop.kind === 'firers'
        ? 'They fire this only round a loop through the root\'s own chain. Click to show them.'
        : `Fires ${node.name} again, a loop back into its own chain. Click to select it.`
    "
    class="flex h-6.5 w-44 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed"
  >
    <UIcon name="i-lucide-repeat" class="size-3 shrink-0" />
    <span v-if="drawn.loop.kind === 'firers'" class="truncate">{{ drawn.loop.count }} more firers</span>
    <span v-else class="truncate font-mono text-11">{{ node.name }}</span>
    <UIcon v-if="drawn.loop.kind === 'firers'" name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
  <div
    v-else-if="node.count"
    :class="STATE"
    class="flex h-6.5 w-44 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed"
  >
    <UIcon name="i-lucide-layers" class="size-3 shrink-0" />
    <span class="truncate">{{ node.count }} more</span>
    <UIcon name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
  <div
    v-else-if="drawn.look.shape === 'frame'"
    :class="[STATE, dashed ? 'border-dashed' : 'bg-(--bg-3)']"
    class="w-49 overflow-hidden rounded-md border border-(--line-2)"
  >
    <div
      class="relative flex h-14 items-center justify-center bg-cover bg-[center_40%] text-(--fg-3)"
      :class="hidden ? HATCHED : !scene && 'bg-(--bg-2)'"
      :style="hidden ? undefined : scene"
    >
      <UIcon v-if="hidden || !scene" :name="icon" class="size-4.5" />
      <span
        class="absolute start-1.5 top-1.5 flex size-4.5 items-center justify-center rounded-full bg-(--bg-1) shadow-[0_0_0_2px_rgb(14_16_20/0.6)]"
      >
        <UIcon :name="drawn.look.icon" class="size-3" :style="{ color: drawn.look.color }" />
      </span>
      <OriginChip
        :origin="node.site.place.origin"
        dot
        class="absolute end-1.5 top-1.5 rounded-full shadow-[0_0_0_2px_rgb(14_16_20/0.6)]"
      />
    </div>
    <div class="flex flex-col pt-1 pe-1.75 pb-1.25 ps-1.75 whitespace-nowrap">
      <b class="truncate text-[11.5px] leading-4 font-medium" :class="node.title ? 'text-(--fg-1)' : 'text-(--fg-3)'">{{
        caption
      }}</b>
      <code class="truncate font-mono text-[10px] leading-3.5 text-(--fg-3)">{{ node.name }}</code>
    </div>
  </div>
  <div
    v-else-if="drawn.look.shape === 'toast'"
    :class="STATE"
    class="flex h-11.5 w-49 items-center gap-2 overflow-hidden rounded-[10px] border border-(--line-2) bg-(--bg-2) ps-1.75 pe-2.5 whitespace-nowrap"
  >
    <UIcon :name="drawn.look.icon" class="size-6 shrink-0" :style="{ color: drawn.look.color }" />
    <div class="flex min-w-0 flex-col">
      <span class="text-[10.5px] leading-3.5 font-medium" :style="{ color: drawn.look.color }">{{ node.what }}</span>
      <span class="truncate text-12 leading-4 text-(--fg-1)">{{ node.title || node.name }}</span>
    </div>
  </div>
  <div
    v-else
    :class="[STATE, dashed ? 'border-dashed' : fill]"
    class="flex h-6.5 w-44 items-center gap-1.5 overflow-hidden rounded-full border border-accented ps-2 pe-2.5 font-mono text-11 whitespace-nowrap"
  >
    <OriginChip :origin="node.site.place.origin" dot />
    <UIcon :name="drawn.look.icon" class="size-3 shrink-0" :style="{ color: drawn.look.color }" />
    <span class="truncate">{{ node.name }}</span>
    <UIcon v-if="node.hardcoded" name="i-lucide-cog" class="ms-auto size-3 shrink-0 text-dimmed" />
  </div>
  <Handle type="target" :position="Position.Left" :connectable="false" />
  <Handle type="source" :position="Position.Right" :connectable="false" />
</template>
