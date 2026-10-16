<script lang="ts">
/** How each coat property is edited, by its lower-cased key; any other key is read-only, as a GUI widget's are. The
 * wiki's Coat of arms modding page. */
export const COAT_FIELDS: Record<string, { input: "texture" | "color" | "pair" | "number" | "regions" }> = {
  pattern: { input: "texture" },
  texture: { input: "texture" },
  color1: { input: "color" },
  color2: { input: "color" },
  color3: { input: "color" },
  color4: { input: "color" },
  color5: { input: "color" },
  position: { input: "pair" },
  scale: { input: "pair" },
  offset: { input: "pair" },
  rotation: { input: "number" },
  depth: { input: "number" },
  mask: { input: "regions" },
};
</script>

<script setup lang="ts">
/**
 * One property of a coat edited as its key's row says: a texture as tiles of its folder's images colored as its layer
 * is, a color as the named colors with their swatches, a pair or a number as typed, a mask as region toggles. Each
 * write is one change of the value in the file's text, through its model; the same value writes nothing.
 */
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import type { Property, Texture } from "@services/internal/draw";
import { textAt } from "../editor/models";
import { namesQuery, texturesQuery } from "../queries";
import { useDrawStore } from "../stores/draw";
import { recolored } from "./recolor";

const {
  workspaceId,
  path,
  property,
  evaluated,
  textureKey,
  texture = undefined,
} = defineProps<{
  workspaceId: string;
  path: string;
  property: Property;
  /** The value with its file's constants and inline math put in, as the picture reads it. */
  evaluated: string;
  /** The key the texture's folder is written by (pattern, colored_emblem, textured_emblem). */
  textureKey: string;
  /** The layer's texture, whose colors the tiles are drawn in; undefined for none. */
  texture?: Texture;
}>();

/** The database whose names a color slot names. */
const NAMED_COLORS = "named_colors";

/** The most tiles the picker draws at once, each colored on a canvas. */
const TILES = 48;

const store = useDrawStore();
const input = computed(() => COAT_FIELDS[property.key.toLowerCase()]?.input);
// the numbers the value holds, as a pair, a number or the regions a mask keeps (NaN for a word that is none), and as
// the inputs show them
const numbers = computed(() => evaluated.split(/\s+/).filter(Boolean).map(Number));
const typedNumbers = computed(() => numbers.value.map((one) => (Number.isFinite(one) ? String(one) : "")));

/** Writes the property's value. */
const write = (text: string) => void store.rewrite(path, property.valueLocation, text);
/** Whether an input holds a number: an emptied one holds none. */
const isNumber = (typed: string) => typed.trim() !== "" && Number.isFinite(Number(typed));
/** A word as the value was written: quoted where it was. */
const quotedLike = (word: string) => (textAt(path, property.valueLocation.range)?.startsWith('"') ? `"${word}"` : word);

// the named colors, with their swatches
const { data: colors } = useQuery(() =>
  namesQuery({ id: workspaceId, database: input.value === "color" ? NAMED_COLORS : "" }),
);
const colorItems = computed(() =>
  (colors.value ?? []).map((one) => ({ label: one.id, value: one.id, color: one.color })),
);
const swatch = computed(() => colorItems.value.find((one) => one.value === property.value)?.color);

// the folder's images, those the search finds drawn as tiles while the picker is open
const isPicking = shallowRef(false);
const search = shallowRef("");
const { data: tiles } = useQuery(() =>
  texturesQuery({ id: workspaceId, key: input.value === "texture" && isPicking.value ? textureKey : "" }),
);
const shown = computed(() =>
  (tiles.value ?? []).filter((one) => one.id.toLowerCase().includes(search.value.toLowerCase())).slice(0, TILES),
);
/** A tile's image colored as the layer is. */
const tileOf = (url: string) => (texture ? recolored({ ...texture, url, regions: [] }) : url);

/** Writes a tile's texture and closes the picker. */
function pick(id: string): void {
  write(quotedLike(id));
  isPicking.value = false;
}

/** Writes a number as typed. */
function writeNumber(typed: string): void {
  if (isNumber(typed)) write(typed.trim());
}

/** Writes a pair, its numbers as typed, the other as it holds (0 where it holds none). */
function writePair(i: number, typed: string): void {
  if (!isNumber(typed)) return;
  const pair = [0, 1].map((k) => (Number.isFinite(numbers.value[k]) ? numbers.value[k] : 0));
  pair[i] = Number(typed);
  write(`{ ${pair.join(" ")} }`);
}

/** Keeps a mask's region or stops keeping it. */
function toggleRegion(region: number): void {
  const kept = new Set(numbers.value.filter(Number.isFinite));
  if (kept.has(region)) kept.delete(region);
  else kept.add(region);
  write(`{ ${[...kept].sort((a, b) => a - b).join(" ")} }`);
}
</script>

<template>
  <USelectMenu
    v-if="input === 'color'"
    :model-value="property.value"
    :items="colorItems"
    value-key="value"
    size="xs"
    class="w-full"
    :search-input="{ placeholder: 'Find a named color' }"
    @update:model-value="(id) => write(quotedLike(String(id)))"
  >
    <template #leading>
      <span class="size-3 rounded-sm border border-default" :style="{ background: swatch }" />
    </template>
    <template #item-leading="{ item }">
      <span class="size-3 rounded-sm border border-default" :style="{ background: item.color }" />
    </template>
  </USelectMenu>
  <UPopover v-else-if="input === 'texture'" v-model:open="isPicking" :content="{ side: 'left', align: 'start' }">
    <UButton
      :label="property.value"
      trailing-icon="i-lucide-images"
      color="neutral"
      variant="outline"
      size="xs"
      class="w-full justify-between font-mono"
      :ui="{ label: 'truncate' }"
    />
    <template #content>
      <div class="flex w-96 flex-col gap-2 p-2">
        <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find a texture" autofocus />
        <div class="grid max-h-96 grid-cols-4 gap-1.5 overflow-auto">
          <button
            v-for="tile in shown"
            :key="tile.id"
            type="button"
            class="flex flex-col items-center gap-1 rounded-md border p-1 hover:bg-elevated"
            :class="tile.id === property.value ? 'border-primary bg-(--accent-soft)' : 'border-transparent'"
            :title="tile.id"
            @click="pick(tile.id)"
          >
            <img v-if="tileOf(tile.url)" :src="tileOf(tile.url)" alt="" class="size-14 object-contain" />
            <span v-else class="size-14 rounded-sm bg-muted" />
            <span class="w-full truncate font-mono text-10.5 text-dimmed">{{ tile.id }}</span>
          </button>
        </div>
      </div>
    </template>
  </UPopover>
  <div v-else-if="input === 'pair'" class="flex gap-1">
    <UInput
      v-for="i in [0, 1]"
      :key="i"
      :model-value="typedNumbers[i] ?? ''"
      type="number"
      step="0.01"
      size="xs"
      class="w-0 grow font-mono"
      @change="(event: Event) => writePair(i, (event.target as HTMLInputElement).value)"
    />
  </div>
  <UInput
    v-else-if="input === 'number'"
    :model-value="typedNumbers[0] ?? ''"
    type="number"
    step="1"
    size="xs"
    class="w-full font-mono"
    @change="(event: Event) => writeNumber((event.target as HTMLInputElement).value)"
  />
  <div v-else-if="input === 'regions'" class="flex gap-1">
    <UButton
      v-for="region in [1, 2, 3]"
      :key="region"
      :label="String(region)"
      size="xs"
      :color="numbers.includes(region) ? 'primary' : 'neutral'"
      :variant="numbers.includes(region) ? 'soft' : 'outline'"
      :aria-pressed="numbers.includes(region)"
      @click="toggleRegion(region)"
    />
  </div>
</template>
