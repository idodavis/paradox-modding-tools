<script setup lang="ts">
/**
 * A mod's thumbnail (pmt.css .thumb): the image at path on a bg-4 tile, or the tile in the mod's color
 * when it has none. 28px; xs 16, sm 20, lg 48 and xl 88 as the boards draw them.
 */
import { useQuery } from "@pinia/colada";
import { imageQuery } from "../../queries";

const { path = "", color = "", size = "md" } = defineProps<{
  path?: string;
  color?: string;
  size?: "xs" | "sm" | "md" | "lg" | "xl";
}>();

const SIZES = { xs: "size-4 rounded-sm", sm: "size-5 rounded-sm", md: "size-7 rounded-md", lg: "size-12 rounded-lg", xl: "size-22 rounded-lg" };
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
