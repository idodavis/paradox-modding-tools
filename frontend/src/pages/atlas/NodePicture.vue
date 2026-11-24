<!-- A node's picture: the game's image, a tinted picture as a shape in the text color, else the icon of what it is. -->
<script lang="ts">
import { Type } from "@internal/lang/models";
import type { Node } from "@services/atlas/models";

/** A Look is how the nodes of one display are drawn in an event's graph. */
export type Look = { shape: "frame" | "toast" | "pill"; filled: boolean; color: string; icon: string };

const EVENT: Look = { shape: "frame", filled: false, color: "var(--blue)", icon: "i-lucide-app-window" };
/** LOOKS holds the look of each display Go names. */
export const LOOKS: Record<string, Look> = {
  window: EVENT,
  hidden: EVENT,
  message: { shape: "toast", filled: false, color: "var(--green)", icon: "i-lucide-message-square" },
  on_action: { shape: "pill", filled: true, color: "var(--green)", icon: "i-lucide-timer" },
  decision: { shape: "pill", filled: false, color: "var(--orange)", icon: "i-lucide-gavel" },
  effect: { shape: "pill", filled: false, color: "var(--purple)", icon: "i-lucide-zap" },
};
/** QUIET is the look of a node of an event's graph with no display. */
export const QUIET: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-box" };
/** HIDDEN is the display of an event the player never sees. */
export const HIDDEN = "hidden";

/** TYPE_ICONS holds the icon of each type of definition, shown where it has no picture. */
export const TYPE_ICONS: Record<string, string> = {
  [Type.TypeGood]: "i-lucide-package",
  [Type.TypeGoodCategory]: "i-lucide-boxes",
  [Type.TypeBuilding]: "i-lucide-factory",
  [Type.TypeProductionMethod]: "i-lucide-cog",
  [Type.TypeProductionMethodGroup]: "i-lucide-layers",
  [Type.TypeTechnology]: "i-lucide-lightbulb",
  [Type.TypeEvent]: "i-lucide-app-window",
};

/** MASK draws a picture the game tints as a shape in the text's color. */
const MASK = { maskSize: "contain", maskRepeat: "no-repeat", maskPosition: "center" };

/** labelOf returns what a node is called: its title, else its name. */
export function labelOf(node: Pick<Node, "title" | "name">): string {
  return node.title || node.name;
}
</script>

<script setup lang="ts">
import { computed } from "vue";

import { fileUrl } from "../../queries";

const { node } = defineProps<{ node: Pick<Node, "picture" | "silhouette" | "type" | "display"> }>();

const look = computed(() => LOOKS[node.display ?? ""]);
</script>

<template>
  <span
    v-if="node.picture && node.silhouette"
    class="block shrink-0 bg-(--fg-1)"
    :style="{ ...MASK, maskImage: `url('${fileUrl(node.picture)}')` }"
  />
  <img
    v-else-if="node.picture"
    :src="fileUrl(node.picture)"
    alt=""
    class="shrink-0"
    :class="look ? 'rounded-xs object-cover' : 'object-contain'"
  />
  <span v-else class="flex shrink-0 items-center justify-center text-(--fg-3)">
    <UIcon
      v-if="look"
      :name="node.display === HIDDEN ? 'i-lucide-eye-off' : look.icon"
      class="size-3/5"
      :style="{ color: look.color }"
    />
    <UIcon v-else :name="TYPE_ICONS[node.type ?? ''] ?? QUIET.icon" class="size-3/5" />
  </span>
</template>
