<!--
  One node of the event graph: an event's frame, a message's toast, or a pill
  for any other definition, a fold or a loop.
-->
<script setup lang="ts">
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";
import { fileUrl } from "../../queries";
import OriginChip from "../../ui/OriginChip.vue";
import FoldPill from "./FoldPill.vue";
import { isPastDepth, NODE_STATE, type Drawn } from "./layout";
import { whatOf } from "./nodes";

const { drawn } = defineProps<{ drawn: Drawn }>();

const HATCHED = "bg-[repeating-linear-gradient(135deg,var(--bg-2)_0_6px,var(--bg-1)_6px_12px)]";

const node = computed(() => drawn.node);
const isHidden = computed(() => node.value.display === "hidden");
const isDashed = computed(() => isPastDepth(node.value));
const fill = computed(() => (drawn.look.filled ? "bg-muted text-highlighted" : "bg-default text-muted"));
const icon = computed(() => (isHidden.value ? "i-lucide-eye-off" : drawn.look.icon));
const scene = computed(() =>
  node.value.picture ? { backgroundImage: `url("${fileUrl(node.value.picture)}")` } : undefined,
);
const caption = computed(() => node.value.title || whatOf(node.value));
const hint = computed(() => (isDashed.value ? "Past the depth. Click to draw what it leads to." : undefined));
/** loopPill is what a loop's pill says: its firers folded, or a firing back. */
const loopPill = computed(() => {
  if (!drawn.loop) return undefined;

  return drawn.loop.marker === "firers"
    ? {
        label: `${drawn.loop.count} more firers`,
        title: "They fire this only round a loop through the root's own chain. Click to show them.",
        isFold: true,
      }
    : {
        label: node.value.name,
        title: `Fires ${node.value.name} again, a loop back into its own chain. Click to select it.`,
        isFold: false,
      };
});
</script>

<template>
  <FoldPill
    v-if="loopPill"
    icon="i-lucide-repeat"
    :label="loopPill.label"
    :mono="!loopPill.isFold"
    :opens="loopPill.isFold"
    :title="loopPill.title"
    class="w-44"
  />
  <FoldPill v-else-if="node.count" icon="i-lucide-layers" :label="`${node.count} more`" class="w-44" />
  <div
    v-else-if="drawn.look.shape === 'frame'"
    :class="[NODE_STATE, isDashed ? 'border-dashed' : 'bg-(--bg-3)']"
    :title="hint"
    class="w-49 overflow-hidden rounded-md border border-(--line-2)"
  >
    <div
      class="relative flex h-14 items-center justify-center bg-cover bg-[center_40%] text-(--fg-3)"
      :class="isHidden ? HATCHED : !scene && 'bg-(--bg-2)'"
      :style="isHidden ? undefined : scene"
    >
      <UIcon v-if="isHidden || !scene" :name="icon" class="size-4.5" />
      <span
        class="absolute start-1.5 top-1.5 flex size-4.5 items-center justify-center rounded-full bg-(--bg-1) shadow-[0_0_0_2px_color-mix(in_srgb,var(--bg-0)_60%,transparent)]"
      >
        <UIcon :name="drawn.look.icon" class="size-3" :style="{ color: drawn.look.color }" />
      </span>
      <OriginChip
        :origin="node.site.place.origin"
        dot
        class="absolute end-1.5 top-1.5 rounded-full shadow-[0_0_0_2px_color-mix(in_srgb,var(--bg-0)_60%,transparent)]"
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
    :class="NODE_STATE"
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
    :class="[NODE_STATE, isDashed ? 'border-dashed' : fill]"
    :title="hint"
    class="flex h-6.5 w-44 items-center gap-1.5 overflow-hidden rounded-full border border-accented ps-2 pe-2.5 text-11 whitespace-nowrap"
  >
    <OriginChip :origin="node.site.place.origin" dot />
    <UIcon :name="drawn.look.icon" class="size-3 shrink-0" :style="{ color: drawn.look.color }" />
    <span class="truncate" :class="!node.title && 'font-mono'">{{ node.title || node.name }}</span>
    <UIcon v-if="node.hardcoded" name="i-lucide-cog" class="ms-auto size-3 shrink-0 text-dimmed" />
  </div>
  <Handle type="target" :position="Position.Left" :connectable="false" />
  <Handle type="source" :position="Position.Right" :connectable="false" />
</template>
