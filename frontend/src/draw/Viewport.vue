<script setup lang="ts">
/**
 * A pane's floor: its content, of a given size in CSS pixels, on the theme's checkerboard, fit to the pane until
 * zoomed (Fit, 100%, − and +, Ctrl+wheel around the pointer). A row under it holds the pane's own controls, then the
 * zoom's buttons. The content is scaled, not resized, so text stays crisp.
 */
import { computed, nextTick, shallowRef, useTemplateRef } from "vue";
import { useElementSize } from "@vueuse/core";

const { width, height } = defineProps<{ width: number; height: number }>();
/** Whether the content fits the pane: true until the user zooms, and again after Fit. */
const fit = defineModel<boolean>("fit", { default: true });

const MIN_ZOOM = 0.02;
const MAX_ZOOM = 32;

const pane = useTemplateRef("pane");
const size = useElementSize(pane);

// the zoom the user chose; a content that fits is shown at the zoom that fits it to the pane, never past its own size
const chosen = shallowRef(1);
const fitted = computed(() =>
  width ? Math.min(1, (size.width.value - 32) / width, (size.height.value - 32) / height) : 1,
);
const zoom = computed(() => Math.max(MIN_ZOOM, fit.value ? fitted.value : chosen.value));

/** Zoom to a level within the limits, which ends the fit; the level it took. */
function zoomTo(level: number): number {
  chosen.value = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, level));
  fit.value = false;
  return chosen.value;
}

// Ctrl+wheel zooms around the pointer: the point under it stays there
function onWheel(e: WheelEvent): void {
  const el = pane.value;
  if (!e.ctrlKey || !el) return;
  e.preventDefault();
  const box = el.getBoundingClientRect();
  const x = e.clientX - box.left;
  const y = e.clientY - box.top;
  const previous = zoom.value;
  const ratio = zoomTo(previous * (e.deltaY < 0 ? 1.25 : 0.8)) / previous;
  void nextTick(() => el.scrollTo((el.scrollLeft + x) * ratio - x, (el.scrollTop + y) * ratio - y));
}

defineExpose({ zoom });
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-(--bg-0)">
    <div
      ref="pane"
      class="relative min-h-0 flex-1 overflow-auto"
      style="background: repeating-conic-gradient(var(--bg-0) 0 25%, var(--bg-4) 0 50%) 0 0 / 16px 16px"
      @wheel="onWheel"
    >
      <div class="flex min-h-full w-max min-w-full items-center justify-center p-4">
        <!-- the scaled box takes the room the scaled content needs, which a transform leaves unchanged -->
        <div class="relative shrink-0" :style="{ width: `${width * zoom}px`, height: `${height * zoom}px` }">
          <div
            class="absolute top-0 left-0 origin-top-left"
            :style="{ width: `${width}px`, height: `${height}px`, transform: `scale(${zoom})` }"
          >
            <slot />
          </div>
        </div>
      </div>
    </div>
    <div
      class="flex h-7 shrink-0 items-center gap-1.5 border-t border-default px-3.5 text-12 whitespace-nowrap text-dimmed tabular-nums"
    >
      <slot name="toolbar" />
      <span class="grow" />
      <UButton label="Fit" variant="ghost" size="xs" :disabled="!width" @click="fit = true" />
      <UButton label="100%" variant="ghost" size="xs" :disabled="!width" @click="zoomTo(1)" />
      <UButton
        icon="i-lucide-minus"
        variant="ghost"
        size="xs"
        square
        aria-label="Zoom out"
        :disabled="!width"
        @click="zoomTo(zoom * 0.8)"
      />
      <UButton
        icon="i-lucide-plus"
        variant="ghost"
        size="xs"
        square
        aria-label="Zoom in"
        :disabled="!width"
        @click="zoomTo(zoom * 1.25)"
      />
    </div>
  </div>
</template>
