<script setup lang="ts">
/**
 * One value of a coat layer, edited as its key's row says: a texture in the texture picker, a color as a chip of the
 * named colors (a later slot cleared by its ×), a pair or a number as typed, a mask as region toggles. A value the
 * layer writes is written anew, one it does not is added to its block (an emblem that writes no instance gets one);
 * each write is one change of the file's text, the same value writing nothing.
 */
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import type { Layer, Property } from "@services/internal/draw";
import { textAt } from "../editor/models";
import { colorsQuery } from "../queries";
import { useDrawStore } from "../stores/draw";
import { placementDefaults } from "./coat";
import TexturePicker from "./TexturePicker.vue";

const {
  workspaceId,
  path,
  field,
  layer,
  property = undefined,
  isReadOnly = false,
} = defineProps<{
  workspaceId: string;
  path: string;
  /** The key it writes, lower-cased. */
  field: string;
  /** The layer it is of: its value, its block (where a key it writes none of is added) and its texture's colors. */
  layer: Layer;
  /** The property that writes it; undefined where the layer writes none. */
  property?: Property;
  /** Shown only: a parent coat writes it, or the coat is not the open file's to write. */
  isReadOnly?: boolean;
}>();

/** How each value is edited, by its key (the wiki's Coat of arms modding page). */
const INPUTS: Record<string, "texture" | "color" | "pair" | "number" | "regions"> = {
  pattern: "texture",
  texture: "texture",
  color1: "color",
  color2: "color",
  color3: "color",
  color4: "color",
  color5: "color",
  position: "pair",
  scale: "pair",
  offset: "pair",
  rotation: "number",
  depth: "number",
  mask: "regions",
};

const store = useDrawStore();
const input = computed(() => INPUTS[field]);
// the value as the picture reads it, its file's constants put in, and the numbers it holds (NaN for a word)
const value = computed(() => layer.properties?.[field] ?? "");
const numbers = computed(() => value.value.split(/\s+/).filter(Boolean).map(Number));
const typedNumbers = computed(() => numbers.value.map((one) => (Number.isFinite(one) ? String(one) : "")));
// what a chip names: the value, else the property as written (a color block holds no scalars)
const chipText = computed(() => value.value || property?.value || "");
// a color the layer writes after its first can be cleared, its line removed: not every emblem paints a second or third
const isClearable = computed(() => input.value === "color" && field !== "color1" && !!property && !isReadOnly);

/** Writes the value: the property's anew, else the key added to the layer's block, inside an instance block where the
 * layer is the one an emblem that writes none has. */
function write(text: string): void {
  if (property) {
    void store.rewrite(path, property.valueLocation, text);
    return;
  }
  const isImplicit = layer.type === "instance" && layer.key !== "instance";
  void store.addInside(path, layer.location, [isImplicit ? `instance = { ${field} = ${text} }` : `${field} = ${text}`]);
}
/** Whether an input holds a number: an emptied one holds none. */
const isNumber = (typed: string) => typed.trim() !== "" && Number.isFinite(Number(typed));
/** A word as the value is written: quoted unless it is written bare. */
const quotedLike = (text: string) =>
  property && !textAt(path, property.valueLocation.range)?.startsWith('"') ? text : `"${text}"`;

// the named colors, with their swatches
const { data: colors } = useQuery(() => colorsQuery({ id: input.value === "color" ? workspaceId : "" }));
const colorItems = computed(() =>
  Object.entries(colors.value ?? {}).map(([id, css]) => ({ label: id, value: id, color: css ?? "" })),
);

// the texture picker, open while picking; the key a texture is written by names its folder: a pattern's, else the
// emblem's own
const isPicking = shallowRef(false);
const textureKey = computed(() => (field === "texture" ? layer.key : field));

/** Writes a tile's texture and closes the picker. */
function pick(file: string): void {
  write(quotedLike(file));
  isPicking.value = false;
}

/** Writes a number as typed. */
function writeNumber(typed: string): void {
  if (isNumber(typed)) write(typed.trim());
}

/** Writes a pair, its number as typed, the other as it holds, else as the layer takes it unwritten (0 where nothing
 * says). */
function writePair(index: number, typed: string): void {
  if (!isNumber(typed)) return;
  const defaults = placementDefaults(layer.type, field);
  const pair = [0, 1].map((one) => (Number.isFinite(numbers.value[one]) ? numbers.value[one] : (defaults[one] ?? 0)));
  pair[index] = Number(typed);
  write(`{ ${pair.join(" ")} }`);
}

/** Keeps a mask's region or stops keeping it; keeping none is the whole field, the mask removed. */
function toggleRegion(region: number): void {
  const regions = new Set(numbers.value.filter(Number.isFinite));
  if (regions.has(region)) regions.delete(region);
  else regions.add(region);
  if (regions.size) write(`{ ${[...regions].sort((first, second) => first - second).join(" ")} }`);
  else if (property) void store.removeBlock(path, property.location);
}
</script>

<template>
  <USelectMenu
    v-if="input === 'color'"
    :model-value="value"
    :items="colorItems"
    value-key="value"
    variant="none"
    size="xs"
    trailing-icon=""
    :disabled="isReadOnly"
    :search-input="{ placeholder: 'Find a named color' }"
    :clear="isClearable"
    :reset-model-value-on-clear="false"
    :ui="{ base: 'p-0', content: 'w-60', trailing: 'static ps-0.5' }"
    @update:model-value="(id) => write(quotedLike(String(id)))"
    @clear="property && store.removeBlock(path, property.location)"
  >
    <span
      class="flex h-5.5 items-center gap-1.25 rounded-md border bg-elevated ps-1 pe-1.75 text-11.5 whitespace-nowrap"
      :class="chipText ? 'border-default text-highlighted' : 'border-dashed border-default text-dimmed'"
    >
      <span
        class="size-3.5 rounded-sm border border-black/35"
        :class="!chipText && 'border-dashed'"
        :style="{ background: chipText ? layer.colors?.[Number(field.slice(5))] : 'transparent' }"
      />{{ chipText || field.slice(5) }}
    </span>
    <template #item-leading="{ item }">
      <span class="size-3 rounded-sm border border-default" :style="{ background: item.color }" />
    </template>
  </USelectMenu>
  <UPopover v-else-if="input === 'texture'" v-model:open="isPicking" :content="{ side: 'left', align: 'start' }">
    <div class="flex min-w-0 items-center gap-2">
      <span class="min-w-0 grow truncate font-mono text-11.5 text-highlighted" :title="value">{{ value }}</span>
      <UButton v-if="!isReadOnly" label="Change" color="neutral" variant="outline" size="xs" />
    </div>
    <template #content>
      <TexturePicker
        :workspace-id="workspaceId"
        :texture-key="textureKey"
        :colors="layer.colors ?? []"
        :value="value"
        @pick="pick"
      />
    </template>
  </UPopover>
  <div v-else-if="input === 'pair'" class="flex gap-1">
    <UInput
      v-for="index in [0, 1]"
      :key="index"
      :model-value="typedNumbers[index] ?? ''"
      type="number"
      step="0.01"
      size="xs"
      :disabled="isReadOnly"
      class="w-0 grow font-mono"
      @change="(event: Event) => writePair(index, (event.target as HTMLInputElement).value)"
    />
  </div>
  <UInput
    v-else-if="input === 'number'"
    :model-value="typedNumbers[0] ?? ''"
    type="number"
    step="1"
    size="xs"
    :disabled="isReadOnly"
    class="w-full font-mono"
    @change="(event: Event) => writeNumber((event.target as HTMLInputElement).value)"
  />
  <UFieldGroup v-else-if="input === 'regions'" size="xs">
    <UButton
      label="Whole field"
      :color="numbers.length ? 'neutral' : 'primary'"
      :variant="numbers.length ? 'outline' : 'soft'"
      :disabled="isReadOnly || !numbers.length"
      @click="property && store.removeBlock(path, property.location)"
    />
    <UButton
      v-for="region in [1, 2, 3]"
      :key="region"
      :label="String(region)"
      :color="numbers.includes(region) ? 'primary' : 'neutral'"
      :variant="numbers.includes(region) ? 'soft' : 'outline'"
      :aria-pressed="numbers.includes(region)"
      :disabled="isReadOnly"
      @click="toggleRegion(region)"
    />
  </UFieldGroup>
</template>
