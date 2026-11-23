<!--
  The icon of a node: a picture the game tints drawn as a mask in the text
  color, its picture, else the glyph of its type or the symbol of its database.
-->
<script setup lang="ts">
import { computed } from "vue";
import { Type } from "@internal/lang/models";
import type { Node } from "@services/graph/models";
import { fileUrl } from "../../queries";
import { lookupLook } from "./layout";
import { TYPE_ICONS } from "./nodes";

const { node } = defineProps<{
  node: Pick<Node, "database" | "picture" | "silhouette" | "display" | "type">;
}>();

const look = computed(() => lookupLook(node.database));
const url = computed(() => `url("${fileUrl(node.picture ?? "")}")`);
</script>

<template>
  <span
    v-if="node.picture && node.silhouette"
    class="block shrink-0 bg-(--fg-1)"
    :style="{ maskImage: url, maskSize: 'contain', maskRepeat: 'no-repeat', maskPosition: 'center' }"
  />
  <img
    v-else-if="node.picture"
    :src="fileUrl(node.picture)"
    alt=""
    class="shrink-0"
    :class="node.type ? 'object-contain' : 'rounded-xs object-cover'"
  />
  <span v-else-if="node.type" class="flex shrink-0 items-center justify-center text-(--fg-3)">
    <UIcon :name="TYPE_ICONS[node.type] ?? TYPE_ICONS[Type.TypeGood]" class="size-3/5" />
  </span>
  <UIcon
    v-else
    :name="node.display === 'hidden' ? 'i-lucide-eye-off' : look.icon"
    class="shrink-0"
    :style="{ color: look.color }"
  />
</template>
