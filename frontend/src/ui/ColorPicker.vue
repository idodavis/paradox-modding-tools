<!-- A color picked from a row of swatches or by hand. Its heading says which, and the swatch picked has a ring. -->
<script setup lang="ts">
import { computed } from "vue";

import ValueField from "./ValueField.vue";

/** A Swatch is one color the picker offers: the value it sets, its CSS background and its name for a screen reader. */
type Swatch = { value: string; background: string; label: string };

const { items, note } = defineProps<{
  items: readonly Swatch[];
  /** What the heading says while the color is one of the swatches. */
  note: string;
}>();
const color = defineModel<string>({ required: true });

/** Whether the color is set and none of the swatches is it, so it was picked by hand. */
const custom = computed(() => !!color.value && !items.some((item) => item.value === color.value));
</script>

<template>
  <div class="flex flex-col gap-3">
    <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
      Color
      <span class="text-12 font-normal text-dimmed">{{ custom ? "Picked by hand" : note }}</span>
    </h3>
    <div class="flex flex-wrap items-center gap-2">
      <button
        v-for="item in items"
        :key="item.value"
        type="button"
        class="size-5.5 shrink-0 rounded-md border border-accented"
        :class="{ 'ring-1 ring-primary ring-offset-2 ring-offset-(--bg-3)': color === item.value }"
        :style="{ background: item.background }"
        :aria-label="item.label"
        @click="color = item.value"
      />
      <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />
      <ValueField
        kind="color"
        :value="custom ? color : ''"
        label="Custom…"
        class="px-0"
        @update:value="color = $event"
      />
    </div>
  </div>
</template>
