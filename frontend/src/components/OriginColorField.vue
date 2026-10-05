<script lang="ts">
/**
 * An origin's color: automatic (a mod's slot hue, the game's near-grey) or a hex picked by hand.
 */
export const ORIGIN_COLOR_DESC =
  "Used for explorer roots, origin chips and filters, and hover origin labels. Each mod keeps the color it got when it joined; a picked color wins.";
</script>

<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import { cssHex } from "../lib/cssHex";

/** The hand-picked hex, or "" for automatic. */
const color = defineModel<string>({ required: true });

const { slot = 0, label = "Origin color", compact = false } = defineProps<{
  /** A mod's slot; none for the game. */
  slot?: number;
  label?: string;
  /** Hide the paragraph; keep the same copy on an info tooltip. */
  compact?: boolean;
}>();

const emit = defineEmits<{ slot: [slot: number] }>();

const SLOTS = [1, 2, 3, 4, 5, 6, 7, 8];
const picker = useTemplateRef("picker");
/** Where the system picker starts: the color shown now. */
const shown = computed(() => color.value || (slot ? `var(--o${slot})` : "var(--game)"));

function onPick(event: Event): void {
  if (event.target instanceof HTMLInputElement) color.value = event.target.value;
}

function pickCustom(): void {
  if (!picker.value) return;
  picker.value.value = cssHex(shown.value);
  picker.value.click();
}
</script>

<template>
  <UFormField :description="compact ? undefined : ORIGIN_COLOR_DESC">
    <template #label>
      <FieldHelpTip v-if="compact" :label="label" :text="ORIGIN_COLOR_DESC" />
      <template v-else>{{ label }}</template>
    </template>
    <div class="flex flex-wrap items-center gap-2">
      <template v-if="slot">
        <button
          v-for="n in SLOTS"
          :key="n"
          type="button"
          class="size-5.5 rounded-md border border-accented outline-offset-2 outline-primary"
          :class="{ outline: !color && slot === n }"
          :style="{ background: `var(--o${n})` }"
          :aria-label="`Color ${n}`"
          @click="emit('slot', n)"
        />
      </template>
      <button
        v-else
        type="button"
        class="size-5.5 rounded-md border border-accented bg-(--game) outline-offset-2 outline-primary"
        :class="{ outline: !color }"
        aria-label="Automatic"
        @click="color = ''"
      />
      <span
        v-if="color"
        class="size-5.5 rounded-md border border-accented outline outline-offset-2 outline-primary"
        :style="{ background: color }"
      />
      <UButton label="Custom…" size="sm" @click="pickCustom" />
      <input ref="picker" type="color" class="sr-only" tabindex="-1" @change="onPick">
    </div>
  </UFormField>
</template>
