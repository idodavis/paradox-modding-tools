<!--
  One node of the canvas. A closed event is a frame with its scene and its origin's dot, a message a toast, any other
  entry a pill and a fold a dashed pill. An open node is its head, over its scene for a window, and a row per firing,
  each row with its own handle. A node the depth stopped at is dashed until the user opens it.
-->
<script setup lang="ts">
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";
import type { Edge } from "@services/graph/models";
import { plural } from "../../lib/format";
import { fileUrl } from "../../queries";
import OriginChip from "../../ui/OriginChip.vue";
import ConditionList from "./ConditionList.vue";
import type { Drawn, Row } from "./layout";

const { drawn, labels = {} } = defineProps<{ drawn: Drawn; labels?: Record<string, string | undefined> }>();
const emit = defineEmits<{ fold: [id: string] }>();

/** The classes that fade a node off the lit route and ring the root and the selected node. */
const STATE =
  "in-[.dim]:opacity-40 in-[.root]:border-primary in-[.root]:ring-3 in-[.root]:ring-(--accent-soft) in-[.selected]:border-secondary in-[.selected]:ring-3 in-[.selected]:ring-(--link-soft)";
/** The hatching of a hidden event's scene and head. */
const HATCHED = "bg-[repeating-linear-gradient(135deg,var(--bg-2)_0_6px,var(--bg-1)_6px_12px)]";
/** The color of each kind of deciding block's key, as the editor colors it. */
const KEYS: Record<string, string> = { branch: "var(--syn-block)", list: "var(--syn-scope)" };

const node = computed(() => drawn.node);
const hidden = computed(() => node.value.display === "hidden");
/** Whether the node is drawn dashed: the depth stopped at it, and it is not the root. */
const outline = computed(() => node.value.deep && node.value.step !== 0);
const icon = computed(() => (hidden.value ? "i-lucide-eye-off" : drawn.look.icon));
/** Whether a row goes on along the selection's route, so that the rest fade. */
const isLit = computed(() => drawn.rows.some((row) => row.lit));
const scene = computed(() =>
  node.value.background ? { backgroundImage: `url("${fileUrl(node.value.background)}")` } : undefined,
);
/** The frame's first line: the title, else what it is. */
const caption = computed(() => {
  if (outline.value) return "Beyond depth, expand to load";
  return node.value.title || (hidden.value ? "hidden event" : node.value.what);
});
/** Describes a row's target: the one node it fires, a message by its title, or how many it fires. */
function describeTarget(fires: Edge[]): string {
  if (fires.length > 1) {
    const database = fires[0].to.split(":")[0];
    return plural(fires.length, labels[database]?.replace(/s$/, "") ?? "entry");
  }
  return fires[0].title || fires[0].to.slice(fires[0].to.indexOf(":") + 1);
}

/** Describes what a row's firing is written in where no block decides it: its first block, else its effect. */
function describePlain(row: Row): string {
  return row.fires[0]?.steps?.[0]?.key ?? row.fires[0]?.effect ?? "";
}

/** Opens the node's "N more" fold. The click stops there, so it does not also select the node. */
function openMore(event: MouseEvent): void {
  event.stopPropagation();
  emit("fold", `more:${node.value.id}`);
}
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
  <div v-else-if="drawn.open" :class="STATE" class="relative w-59 rounded-lg border border-(--line-2) bg-(--bg-3)">
    <div
      class="relative isolate flex flex-col gap-px rounded-t-lg py-1.75 ps-2.75 pe-2.5 whitespace-nowrap"
      :class="[
        drawn.rows.length && 'border-b border-(--line-1)',
        hidden && HATCHED,
        !drawn.rows.length && 'rounded-b-lg',
      ]"
    >
      <span
        v-if="scene && !hidden"
        class="absolute inset-0 -z-1 rounded-[inherit] bg-cover bg-[center_40%] [mask:linear-gradient(90deg,transparent_30%,#000_92%)]"
        :style="scene"
      />
      <span class="flex h-3.5 items-center gap-1.25 text-[10.5px] font-medium" :style="{ color: drawn.look.color }">
        <UIcon :name="icon" class="size-3" />{{ node.what }}{{ hidden ? ", hidden" : "" }}
        <span
          v-if="node.hardcoded"
          class="inline-flex h-4 items-center gap-0.75 rounded-sm bg-(--bg-4) px-1.25 text-[10px] text-(--fg-3)"
          title="The game calls it from its own code"
          ><UIcon name="i-lucide-cog" class="size-2.5" />game</span
        >
        <span class="grow" />
        <OriginChip :origin="node.place.origin" dot />
      </span>
      <span class="truncate font-mono text-12 leading-4.25 font-semibold text-(--fg-1)">{{ node.name }}</span>
      <span v-if="node.title && node.display === 'window'" class="truncate text-[11.5px] leading-3.75 text-(--fg-2)">{{
        node.title
      }}</span>
      <span v-if="node.comment" class="truncate text-11 leading-3.75 text-(--fg-3) italic" :title="node.comment">{{
        node.comment.split("\n")[0]
      }}</span>
    </div>
    <div v-if="drawn.rows.length" class="flex flex-col py-1">
      <div
        v-for="row in drawn.rows"
        :key="row.id"
        class="relative flex h-5.5 min-w-0 items-center gap-1.5 pe-3.5 text-11 whitespace-nowrap text-(--fg-2)"
        :class="[
          row.nested ? 'ps-5.75' : 'ps-2.75',
          row.header && 'text-(--fg-3)',
          row.more && 'cursor-pointer',
          row.lit ? 'bg-(--link-soft) text-(--fg-1)' : isLit && 'opacity-45',
        ]"
        @click="row.more && openMore($event)"
      >
        <template v-if="row.more">
          <UIcon name="i-lucide-layers" class="size-3 shrink-0 text-dimmed" />
          <span class="text-dimmed">{{ row.more }} more</span>
        </template>
        <template v-else>
          <span
            v-if="row.step?.kind === 'weight'"
            class="inline-flex h-4 min-w-7 shrink-0 items-center justify-center rounded-sm bg-(--blue-soft) px-1.5 text-[10px] font-semibold text-(--blue) tabular-nums"
            :title="row.step.modifiers ? `${row.step.modifiers} blocks change the weight` : undefined"
            >{{ row.step.weight }}{{ row.step.modifiers ? "±" : "" }}</span
          >
          <span v-if="row.step" class="shrink-0" :style="{ color: KEYS[row.step.kind] ?? 'var(--fg-3)' }">{{
            row.step.key
          }}</span>
          <span v-else class="shrink-0 text-(--fg-3)">{{ describePlain(row) }}</span>
          <ConditionList
            v-if="row.step?.when?.length"
            :conditions="row.step.when"
            inline
            class="flex-1 text-[10.5px]"
          />
          <code
            v-if="row.scopes.length"
            class="min-w-0 flex-1 truncate font-mono text-[10.5px] text-(--fg-3)"
            :title="row.scopes.map((scope) => scope.key).join(' › ')"
            >in {{ row.scopes.map((scope) => scope.key).join(" › ") }}</code
          >
          <template v-if="!row.header">
            <span v-if="row.delay" class="shrink-0 text-(--fg-3)">{{ row.delay }}</span>
            <span
              class="ms-auto min-w-0 truncate font-mono text-[10.5px] text-(--fg-1)"
              :title="describeTarget(row.fires)"
              >{{ describeTarget(row.fires) }}</span
            >
            <Handle :id="row.id" type="source" :position="Position.Right" :connectable="false" />
          </template>
        </template>
      </div>
    </div>
    <Handle id="node" type="source" :position="Position.Right" :connectable="false" />
  </div>
  <div
    v-else-if="drawn.look.shape === 'frame'"
    :class="[STATE, outline ? 'border-dashed' : 'bg-(--bg-3)']"
    class="w-49 overflow-hidden rounded-md border border-(--line-2)"
  >
    <div
      class="relative flex h-14 items-center justify-center bg-cover bg-[center_40%] text-(--fg-3)"
      :class="hidden ? HATCHED : !scene && 'bg-(--bg-2)'"
      :style="hidden ? undefined : scene"
    >
      <UIcon v-if="hidden || !scene" :name="icon" class="size-4.5" />
      <OriginChip
        :origin="node.place.origin"
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
    :class="[
      STATE,
      outline ? 'border-dashed' : drawn.look.filled ? 'bg-muted text-highlighted' : 'bg-default text-muted',
    ]"
    class="flex h-6.5 w-44 items-center gap-1.5 overflow-hidden rounded-full border border-accented ps-2 pe-2.5 font-mono text-11 whitespace-nowrap"
  >
    <OriginChip :origin="node.place.origin" dot />
    <UIcon :name="drawn.look.icon" class="size-3 shrink-0" :style="{ color: drawn.look.color }" />
    <span class="truncate">{{ node.name }}</span>
    <UIcon v-if="node.hardcoded" name="i-lucide-cog" class="ms-auto size-3 shrink-0 text-dimmed" />
  </div>
  <Handle type="target" :position="Position.Left" :connectable="false" />
  <Handle v-if="!drawn.open" type="source" :position="Position.Right" :connectable="false" />
</template>
