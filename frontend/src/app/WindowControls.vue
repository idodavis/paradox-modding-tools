<!--
  The window's minimize, maximize and close buttons, drawn the way the system draws its own: square on Windows,
  round on Linux. On Windows each button tells the system which one it is, so maximize shows the Snap Layouts.
-->
<script setup lang="ts">
import { useDebounceFn, useEventListener } from "@vueuse/core";
import { System, Window } from "@wailsio/runtime";
import { computed, shallowRef } from "vue";

/** Whether the buttons are Linux's round ones rather than Windows' square ones. */
const ROUND = System.IsLinux();
const SQUARE_BUTTON = "flex w-11.5 items-center justify-center text-toned";
const ROUND_BUTTON =
  "flex size-6 items-center justify-center rounded-full bg-accented text-toned hover:text-highlighted";

const maximized = shallowRef(false);
const checkMaximized = useDebounceFn(async () => (maximized.value = await Window.IsMaximised()), 100);
useEventListener(window, "resize", () => void checkMaximized());
void checkMaximized();

/** The buttons in order: the system's name for each, its label, its glyph on a 10 by 10 grid and what it does. */
const buttons = computed(() => [
  { region: "minimize", label: "Minimize", glyph: "M0 5.5h10", press: () => Window.Minimise() },
  {
    region: "maximize",
    label: maximized.value ? "Restore" : "Maximize",
    glyph: maximized.value ? "M2.5 2.5V.5h7v7h-2M.5 2.5h7v7h-7z" : "M.5.5h9v9h-9z",
    press: () => Window.ToggleMaximise(),
  },
  { region: "close", label: "Close", glyph: "M.5.5l9 9M9.5.5l-9 9", press: () => Window.Close() },
]);

/** styleButton returns a button's classes: round on Linux, square on Windows, with close red under the pointer. */
function styleButton(region: string): string[] {
  if (ROUND) return [ROUND_BUTTON];
  const hover =
    region === "close" ? "hover:bg-(--window-close) hover:text-white" : "hover:bg-accented hover:text-highlighted";
  return [SQUARE_BUTTON, hover];
}
</script>

<template>
  <div class="-my-px flex shrink-0 self-stretch" :class="ROUND && 'items-center gap-2.5 pe-2.5'">
    <button
      v-for="button in buttons"
      :key="button.region"
      type="button"
      :aria-label="button.label"
      :class="styleButton(button.region)"
      :style="{ '--wails-non-client-region': button.region }"
      @click="button.press()"
    >
      <svg
        viewBox="0 0 10 10"
        class="fill-none stroke-current"
        :class="ROUND ? 'size-2 stroke-[1.5]' : 'size-2.5 stroke-1'"
        aria-hidden="true"
      >
        <path :d="button.glyph" />
      </svg>
    </button>
  </div>
</template>
