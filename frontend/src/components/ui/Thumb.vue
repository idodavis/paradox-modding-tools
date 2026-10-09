<script setup lang="ts">
/**
 * A mod's thumbnail (pmt.css .thumb): the image at path on a bg-4 tile, or the tile in the mod's color
 * when it has none. 28px; lg 48, xl 88 and 2xl 104 as the boards draw them; fill, its
 * parent's box (a Workshop screenshot).
 */
import { useQuery } from "@pinia/colada";
import { imageQuery } from "../../queries";

const {
  path = "",
  color = "",
  size = "md",
} = defineProps<{
  path?: string;
  color?: string;
  size?: "md" | "lg" | "xl" | "2xl" | "fill";
}>();

const SIZES = {
  md: "size-7 rounded-md",
  lg: "size-12 rounded-lg",
  xl: "size-22 rounded-lg",
  "2xl": "size-26 rounded-lg",
  fill: "size-full rounded-md",
};
const { data: image } = useQuery(() => imageQuery(path));
</script>

<template>
  <span
    class="shrink-0 overflow-hidden border border-accented bg-accented"
    :class="SIZES[size]"
    :style="!image && color ? { background: color } : undefined"
    data-art
  >
    <img v-if="image" :src="image.src" alt="" class="size-full object-cover" />
  </span>
</template>
