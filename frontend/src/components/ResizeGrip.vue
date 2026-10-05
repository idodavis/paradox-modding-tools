<script setup lang="ts">
/**
 * The resize grip on a pane's edge (pmt.css .rzv and .rzh): 4 by 32 on the border, it and the border
 * accent on hover and while dragging; a double-click restores the default size. It takes no room, so it
 * sits between the two panes it divides. The size is the pane's on its start side, or on its end side
 * when reverse.
 */
import { shallowRef } from "vue";

const size = defineModel<number>({ required: true });
const { axis = "x", reverse = false, min, max, defaultSize } = defineProps<{
  axis?: "x" | "y";
  reverse?: boolean;
  min: number;
  max: number;
  defaultSize: number;
}>();

const drag = shallowRef<{ from: number; size: number }>();

function at(e: PointerEvent): number {
  return axis === "x" ? e.clientX : e.clientY;
}

function start(e: PointerEvent): void {
  if (!(e.currentTarget instanceof HTMLElement)) return;
  e.preventDefault();
  e.currentTarget.setPointerCapture(e.pointerId);
  drag.value = { from: at(e), size: size.value };
}

function move(e: PointerEvent): void {
  if (!drag.value) return;
  const moved = at(e) - drag.value.from;
  size.value = Math.round(Math.min(max, Math.max(min, drag.value.size + (reverse ? -moved : moved))));
}
</script>

<template>
  <div class="relative z-10 shrink-0" :class="axis === 'x' ? 'w-0 self-stretch' : 'h-0'">
    <div
      class="group absolute flex items-center justify-center"
      :class="axis === 'x' ? 'inset-y-0 -start-1 w-2 cursor-col-resize' : 'inset-x-0 -top-1 h-2 cursor-row-resize'"
      @pointerdown="start"
      @pointermove="move"
      @pointerup="drag = undefined"
      @lostpointercapture="drag = undefined"
      @dblclick="size = defaultSize"
    >
      <span
        class="absolute bg-primary"
        :class="[axis === 'x' ? 'inset-y-0 start-1 w-px' : 'inset-x-0 top-1 h-px', drag ? '' : 'opacity-0 group-hover:opacity-100']"
      />
      <span
        class="relative rounded-xs"
        :class="[axis === 'x' ? 'h-8 w-1' : 'h-1 w-8', drag ? 'bg-primary' : 'bg-accented group-hover:bg-primary']"
      />
    </div>
  </div>
</template>
