<script setup lang="ts">
/**
 * A coat texture picker: the images of the folder a key writes textures in, those the search finds drawn as tiles
 * colored as the layer that paints them is, at tile size; a click picks a tile's file. The search waits for typing to
 * pause, so a word typed colors only the tiles it finds.
 */
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { texturesQuery } from "../queries";
import { recolored } from "./coat";

const {
  workspaceId,
  textureKey,
  colors,
  value = "",
} = defineProps<{
  workspaceId: string;
  /** The key a texture is written by, which names its folder and how its tiles are colored. */
  textureKey: string;
  /** The color slots the tiles are colored with, the layer's that paints the texture. */
  colors: string[];
  /** The file the layer paints now; "" none. */
  value?: string;
}>();
const emit = defineEmits<{ pick: [file: string] }>();

/** The most tiles drawn at once, and the size in pixels a tile's colored copy is made at. */
const TILES = 60;
const TILE_SIZE = 96;

const search = shallowRef("");
const query = refDebounced(search, 200);
const { data: textures } = useQuery(() => texturesQuery({ id: workspaceId, key: textureKey }));
const matches = computed(() =>
  Object.entries(textures.value ?? {})
    .flatMap(([file, url]) => (url && file.toLowerCase().includes(query.value.toLowerCase()) ? [{ file, url }] : []))
    .slice(0, TILES),
);
// each tile's colored copy, once made
const tiles = computed(() =>
  matches.value.map(({ file, url }) => ({
    file,
    image: recolored({ url, recolor: textureKey, colors, regions: [] }, TILE_SIZE),
  })),
);
</script>

<template>
  <div class="flex w-[36rem] flex-col gap-2 p-2">
    <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find a texture" autofocus />
    <div class="grid max-h-96 grid-cols-5 gap-1.5 overflow-auto">
      <button
        v-for="{ file, image } in tiles"
        :key="file"
        type="button"
        class="flex min-w-0 flex-col items-center gap-1 rounded-md border p-1 hover:bg-elevated"
        :class="file === value ? 'border-primary bg-(--accent-soft)' : 'border-transparent'"
        :title="file"
        @click="emit('pick', file)"
      >
        <img v-if="image" :src="image" alt="" class="size-16 object-contain" />
        <span v-else class="size-16 rounded-sm bg-muted" />
        <span class="line-clamp-2 w-full font-mono text-10.5 break-all text-dimmed">{{ file }}</span>
      </button>
    </div>
  </div>
</template>
