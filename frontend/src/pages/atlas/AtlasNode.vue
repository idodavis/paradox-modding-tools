<!-- One node of any canvas: an event's frame, a toast, a pill, a card, a group, a fold, a technology or a building. -->
<script lang="ts">
import { Relation } from "@internal/index/models";
import { Type } from "@internal/lang/models";
import type { Fact, Node } from "@services/atlas/models";
import { HIDDEN, labelOf, LOOKS, QUIET } from "./NodePicture.vue";

/** A Kind is what a canvas is about, by its subject's type. */
export type Kind = "event" | "good" | "building" | "tree";
/** A Shape is how a node is drawn. */
export type Shape =
  | "frame"
  | "toast"
  | "pill"
  | "fold"
  | "rest"
  | "loop"
  | "marker"
  | "subject"
  | "building"
  | "card"
  | "group"
  | "member"
  | "good"
  | "technology";

/** The starts of the ids Go makes for the nodes that stand for several: a fold, a loop's fold, a marker, a group. */
export const FOLD = "more";
export const LOOP = "loop:";
export const MARKER = "back:";
const GROUPS = ["in:", "out:"];
/** GOODS are the types of a good and of a category of goods. */
export const GOODS: (Type | undefined)[] = [Type.TypeGood, Type.TypeGoodCategory];

/** isGroup reports whether an id is a group's on a good's canvas. */
export function isGroup(id: string): boolean {
  return GROUPS.some((start) => id.startsWith(start));
}

/**
 * shapeOf returns how a node of a canvas is drawn. It reads the payload alone: the start of the node's id, its
 * display, its type, and whether it is the subject or a group's member.
 */
export function shapeOf(node: Node, kind: Kind, root: string): Shape {
  if (node.id.startsWith(LOOP)) return "loop";
  if (node.id.startsWith(MARKER)) return "marker";
  if (node.id.startsWith(FOLD)) return kind === "event" ? "fold" : "rest";
  if (isGroup(node.id)) return "group";
  if (kind === "tree") return "technology";
  if (kind === "event") return (LOOKS[node.display ?? ""] ?? QUIET).shape;
  if (node.id === root) return node.line ? "building" : "subject";
  if (GOODS.includes(node.type)) return "good";
  if (node.type === Type.TypeTechnology) return "technology";
  return isGroup(node.parent) ? "member" : "card";
}

/** RELATIONS holds what a wire of each relation says before its amount, and the color of what it makes. */
export const RELATIONS: Partial<Record<Relation, { words: string; color: string }>> = {
  [Relation.RelationOutput]: { words: "produces", color: "text-(--blue)" },
  [Relation.RelationBoost]: { words: "boosts", color: "text-(--purple)" },
  [Relation.RelationGives]: { words: "gives", color: "text-(--purple)" },
  [Relation.RelationInput]: { words: "takes", color: "" },
};
</script>

<script setup lang="ts">
import { computed } from "vue";
import { Handle, Position } from "@vue-flow/core";

import { describeCount, toSentence } from "../../lib/format";
import { fileUrl } from "../../queries";
import OriginChip from "../../ui/OriginChip.vue";
import AtlasBuilding from "./AtlasBuilding.vue";
import NodePicture from "./NodePicture.vue";

const {
  node,
  shape,
  amount = "",
  audience = "",
  mark = "",
  selected = "",
  isSubject = false,
  isSelected = false,
  isOpen = false,
  isDown = false,
} = defineProps<{
  node: Node;
  shape: Shape;
  /** How much of a good its wire carries, on the good's pill. */
  amount?: string;
  /** The name of whom an advance is for, "" for one every country can have. */
  audience?: string;
  /** A technology's mark in its tree: the subject, one of its chain, or a root. */
  mark?: string;
  /** The id of the canvas's selection, which a building marks among its levels. */
  selected?: string;
  /** Whether the node is the canvas's subject, and whether it is the selection. */
  isSubject?: boolean;
  isSelected?: boolean;
  /** Whether a group is opened. */
  isOpen?: boolean;
  /** Whether the canvas reads top to bottom. */
  isDown?: boolean;
}>();
const emit = defineEmits<{
  pick: [id: string];
  select: [id: string];
  /** One of a category's goods was clicked, to be the subject. */
  ask: [id: string];
}>();

/** The ring of the subject, in the second color, or of the selection, in gold. An opened group has none. */
const state = computed(() => {
  if (isSelected) return "border-primary ring-3 ring-(--accent-soft)";
  return isSubject ? "border-secondary ring-3 ring-(--link-soft)" : "";
});
const HATCHED = "bg-[repeating-linear-gradient(135deg,var(--bg-2)_0_6px,var(--bg-1)_6px_12px)]";
const DASHED =
  "flex h-6.5 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed";
const CARD = "relative flex items-center gap-2.5 rounded-lg border border-(--line-2) ps-2.5 pe-5";
const NAME = "block truncate font-medium text-(--fg-1)";
const LINE = "block truncate text-11 leading-4 text-(--fg-3)";
const DOT = "absolute end-1.5 top-1.5";
/** MARKS holds the classes of a technology's card by its mark in the tree. */
const MARKS: Record<string, string> = {
  subject: "border-primary ring-3 ring-(--accent-soft)",
  chain: "border-secondary",
  root: "border-(--fg-3) bg-(--bg-3)",
};
/** The most facts a card shows as chips, and the most goods a category shows as buttons. */
const CHIPS = 3;
const MEMBERS = 8;

const look = computed(() => LOOKS[node.display ?? ""] ?? QUIET);
const isHidden = computed(() => node.display === HIDDEN);
/** The depth stopped at a node that leads further, never the subject. */
const isDashed = computed(() => node.deep && node.distance !== 0);
const hint = computed(() => (isDashed.value ? "Past the depth. Click to draw what it leads to." : undefined));
const scene = computed(() => (node.picture ? { backgroundImage: `url('${fileUrl(node.picture)}')` } : undefined));
const caption = computed(() => node.title || (isHidden.value ? "hidden event" : node.what));

/** colorOf returns the color of a fact's amount, by the wire that says the same words. */
function colorOf(fact: Fact): string {
  return Object.values(RELATIONS).find((relation) => relation.words === fact.label)?.color ?? "";
}
</script>

<template>
  <div
    v-if="shape === 'fold'"
    :class="DASHED"
    class="w-44"
    :title="`${describeCount(node.count)} more ${node.what}. Click to show them.`"
  >
    <UIcon name="i-lucide-layers" class="size-3 shrink-0" />
    <span class="truncate">{{ describeCount(node.count) }} more</span>
  </div>
  <div v-else-if="shape === 'rest'" :class="DASHED" class="w-69.5 justify-center">
    {{ describeCount(node.count) }} more, listed in the detail pane
  </div>
  <div
    v-else-if="shape === 'loop'"
    :class="[DASHED, state]"
    class="w-44"
    title="They fire this only round a loop through the root's own chain. Click to show them."
  >
    <UIcon name="i-lucide-repeat" class="size-3 shrink-0" />
    <span class="truncate">{{ describeCount(node.count) }} more firers</span>
    <UIcon name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
  <div
    v-else-if="shape === 'marker'"
    :class="[DASHED, state]"
    class="w-44"
    :title="`Fires ${labelOf(node)} again, a loop back into its own chain. Click to select it.`"
  >
    <UIcon name="i-lucide-repeat" class="size-3 shrink-0" />
    <span class="truncate" :class="!node.title && 'font-mono text-11'">{{ labelOf(node) }}</span>
  </div>
  <div
    v-else-if="shape === 'frame'"
    :class="[state, isDashed ? 'border-dashed' : 'bg-(--bg-3)']"
    :title="hint ?? labelOf(node)"
    class="w-49 overflow-hidden rounded-md border border-(--line-2)"
  >
    <div
      class="relative flex h-14 items-center justify-center bg-cover bg-[center_40%] text-(--fg-3)"
      :class="isHidden ? HATCHED : !scene && 'bg-(--bg-2)'"
      :style="isHidden ? undefined : scene"
    >
      <UIcon v-if="isHidden || !scene" :name="isHidden ? 'i-lucide-eye-off' : look.icon" class="size-4.5" />
      <span
        class="absolute start-1.5 top-1.5 flex size-4.5 items-center justify-center rounded-full bg-(--bg-1) shadow-[0_0_0_2px_color-mix(in_srgb,var(--bg-0)_60%,transparent)]"
      >
        <UIcon :name="look.icon" class="size-3" :style="{ color: look.color }" />
      </span>
      <OriginChip
        :origin="node.origin"
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
    v-else-if="shape === 'toast'"
    :class="state"
    :title="labelOf(node)"
    class="relative flex h-11.5 w-49 items-center gap-2 overflow-hidden rounded-[10px] border border-(--line-2) bg-(--bg-2) ps-1.75 pe-5 whitespace-nowrap"
  >
    <UIcon :name="look.icon" class="size-6 shrink-0" :style="{ color: look.color }" />
    <div class="flex min-w-0 flex-col">
      <span class="text-[10.5px] leading-3.5 font-medium" :style="{ color: look.color }">{{ node.what }}</span>
      <span class="truncate text-12 leading-4 text-(--fg-1)">{{ labelOf(node) }}</span>
    </div>
    <OriginChip :origin="node.origin" dot :class="DOT" />
  </div>
  <div
    v-else-if="shape === 'pill'"
    :class="[state, isDashed ? 'border-dashed' : look.filled ? 'bg-(--bg-2)' : 'bg-(--bg-0)']"
    :title="hint ?? labelOf(node)"
    class="flex h-6.5 w-44 items-center gap-1.5 overflow-hidden rounded-full border border-accented ps-2 pe-2.5 text-11 whitespace-nowrap"
  >
    <OriginChip :origin="node.origin" dot />
    <UIcon :name="look.icon" class="size-3 shrink-0" :style="{ color: look.color }" />
    <span class="truncate" :class="!node.title && 'font-mono'">{{ labelOf(node) }}</span>
    <UIcon v-if="node.hardcoded" name="i-lucide-cog" class="ms-auto size-3 shrink-0 text-dimmed" />
  </div>
  <div
    v-else-if="shape === 'building' && node.line"
    :class="[state, node.line.groups?.length ? 'w-190' : 'w-max min-w-140']"
    class="overflow-hidden rounded-[10px] border border-(--line-2) bg-(--bg-2)"
  >
    <AtlasBuilding
      :node="node"
      :line="node.line"
      :selected="selected"
      @pick="emit('pick', $event)"
      @select="emit('select', $event)"
    />
  </div>
  <div
    v-else-if="shape === 'subject'"
    :class="[CARD, state]"
    :title="labelOf(node)"
    class="h-23 w-75 gap-3.5 bg-[color-mix(in_srgb,var(--link)_7%,var(--bg-2))] ps-3.5"
  >
    <NodePicture :node="node" class="size-14" />
    <span class="min-w-0 flex-1">
      <b class="block truncate text-16 leading-6 font-semibold text-(--fg-1)" :class="!node.title && 'font-mono'">{{
        labelOf(node)
      }}</b>
      <small :class="LINE">{{ toSentence(node.what) }}</small>
      <small v-if="node.summary" :class="LINE">{{ node.summary }}</small>
      <span v-if="node.members?.length" class="flex items-center gap-1 pt-0.5">
        <button
          v-for="member in node.members.slice(0, MEMBERS)"
          :key="member.id"
          type="button"
          :title="labelOf(member)"
          :aria-label="labelOf(member)"
          class="flex size-6 cursor-pointer items-center justify-center rounded-md border border-(--line-2) bg-(--bg-1) hover:bg-(--bg-4)"
          @click.stop="emit('ask', member.id)"
        >
          <NodePicture :node="member" class="size-4.5" />
        </button>
        <small v-if="node.members.length > MEMBERS" class="text-11 text-(--fg-3) tabular-nums">
          +{{ describeCount(node.members.length - MEMBERS) }}
        </small>
      </span>
    </span>
    <OriginChip :origin="node.origin" dot :class="DOT" />
  </div>
  <div
    v-else-if="shape === 'group'"
    :class="[CARD, state]"
    :title="labelOf(node)"
    class="h-13 w-64.5 bg-(--bg-3)"
    :aria-expanded="isOpen"
  >
    <NodePicture :node="node" class="size-6.5" />
    <b :class="NAME" class="min-w-0 flex-1 text-13">{{ labelOf(node) }}</b>
    <span class="rounded-[5px] bg-(--bg-4) px-1.75 text-11 leading-4.5 font-medium text-(--fg-2) tabular-nums">{{
      describeCount(node.count)
    }}</span>
    <UIcon :name="isOpen ? 'i-lucide-chevron-left' : 'i-lucide-chevron-right'" class="size-3.5 text-(--fg-3)" />
    <OriginChip v-if="node.origin" :origin="node.origin" dot :class="DOT" />
  </div>
  <div
    v-else-if="shape === 'good'"
    :class="state"
    :title="node.summary ? `${labelOf(node)}: ${node.summary}` : labelOf(node)"
    class="flex h-8 w-41 items-center gap-2 rounded-full border border-(--line-2) bg-(--bg-3) ps-1.5 pe-2.5 text-12"
  >
    <OriginChip :origin="node.origin" dot class="ms-1" />
    <NodePicture :node="node" class="size-5.5" />
    <b :class="NAME" class="min-w-0 flex-1">{{ labelOf(node) }}</b>
    <span class="shrink-0 font-semibold text-(--blue) tabular-nums">{{ amount }}</span>
  </div>
  <div
    v-else-if="shape === 'technology'"
    :title="audience ? `${labelOf(node)}, for ${audience}` : labelOf(node)"
    class="relative flex h-11 w-54 items-center gap-2.25 rounded-lg border border-(--line-2) bg-(--bg-2) px-2.25"
    :class="[MARKS[mark], audience && 'border-s-3 border-s-(--purple)']"
  >
    <NodePicture :node="node" class="size-6.5" />
    <span class="min-w-0 flex-1">
      <b
        class="block truncate text-[12.5px] leading-4.25 font-medium text-(--fg-1)"
        :class="!node.title && 'font-mono'"
        >{{ labelOf(node) }}</b
      >
      <small class="block truncate text-[10.5px] leading-3.5 text-(--fg-3)">{{ node.summary }}</small>
    </span>
    <span
      v-if="node.limit"
      class="max-w-20 shrink-0 truncate rounded-[4px] bg-(--purple)/16 px-1.25 text-[10.5px] leading-4 text-(--purple)"
      :title="node.limit"
    >
      {{ node.limit }}
    </span>
    <OriginChip :origin="node.origin" dot :class="DOT" />
  </div>
  <div
    v-else
    :class="[CARD, state, shape === 'member' ? 'h-14 w-69.5' : 'h-15.5 w-85']"
    :title="labelOf(node)"
    class="bg-(--bg-2)"
  >
    <NodePicture :node="node" class="size-7.5" />
    <span class="min-w-0 flex-1">
      <b :class="[NAME, !node.title && 'font-mono']" class="text-13">{{ labelOf(node) }}</b>
      <small :class="LINE">{{ node.summary }}</small>
    </span>
    <span v-if="node.facts?.length" class="flex shrink-0 gap-1.5">
      <span
        v-for="fact in node.facts.slice(0, CHIPS)"
        :key="`${fact.label}|${fact.text}`"
        :title="fact.when"
        class="flex flex-col items-end rounded-md border px-1.75 py-0.5 tabular-nums"
        :class="fact.when ? 'border-dashed border-(--line-2)' : 'border-transparent bg-(--bg-1)'"
      >
        <b class="text-12 leading-4 font-semibold" :class="colorOf(fact)">{{ fact.text }}</b>
        <small class="text-[10.5px] leading-3.25 text-(--fg-3)">{{ fact.label }}</small>
      </span>
    </span>
    <OriginChip :origin="node.origin" dot :class="DOT" />
  </div>
  <Handle type="target" :position="isDown ? Position.Top : Position.Left" :connectable="false" />
  <Handle type="source" :position="isDown ? Position.Bottom : Position.Right" :connectable="false" />
</template>
