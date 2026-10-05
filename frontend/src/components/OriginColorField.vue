<script setup lang="ts">
/**
 * An origin's color (the Settings board's Color group): a mod's eight slot swatches, or the game's own
 * near-grey, then Custom…, a hex picked by hand, which wins over either.
 */
import { computed, useTemplateRef } from "vue";
import { cssHex } from "../lib/cssHex";

/** The hand-picked hex, or "" for the slot (a mod) or the near-grey (the game). */
const color = defineModel<string>({ required: true });
/** A mod's slot; none for the game. */
const { slot = 0 } = defineProps<{ slot?: number }>();
const emit = defineEmits<{ slot: [slot: number] }>();

const SLOTS = [1, 2, 3, 4, 5, 6, 7, 8];
const SWATCH = "size-5.5 shrink-0 rounded-md border border-accented";
const ON = "ring-1 ring-primary ring-offset-2 ring-offset-(--bg-3)";
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
  <div class="flex flex-wrap items-center gap-2">
    <template v-if="slot">
      <button
        v-for="n in SLOTS"
        :key="n"
        type="button"
        :class="[SWATCH, { [ON]: !color && slot === n }]"
        :style="{ background: `var(--o${n})` }"
        :aria-label="`Color ${n}`"
        @click="emit('slot', n)"
      />
    </template>
    <button
      v-else
      type="button"
      :class="[SWATCH, 'bg-(--game)', { [ON]: !color }]"
      aria-label="Automatic"
      @click="color = ''"
    />
    <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />
    <UButton label="Custom…" size="sm" @click="pickCustom" />
    <input ref="picker" type="color" class="sr-only" tabindex="-1" @change="onPick">
  </div>
</template>
