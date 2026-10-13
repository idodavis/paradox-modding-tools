<script setup lang="ts">
/**
 * A pane's floor: its content, of a given size in CSS pixels, on the theme's checkerboard, under a camera (Panzoom):
 * dragged anywhere to pan, Ctrl+wheel to zoom around the pointer, the wheel alone to pan; fit to the pane until the
 * user moves it, and again after Fit (100%, − and + zoom around the middle). A row under it holds the pane's own
 * controls, then the zoom's buttons. The content is scaled, not resized, so text stays crisp.
 */
import { computed, onMounted, onScopeDispose, shallowRef, useTemplateRef, watch } from "vue";
import Panzoom, { type PanzoomObject } from "@panzoom/panzoom";
import { useElementSize } from "@vueuse/core";

const { width, height } = defineProps<{ width: number; height: number }>();
/** Whether the content fits the pane: true until the user moves it, and again after Fit. */
const fit = defineModel<boolean>("fit", { default: true });

const MIN_ZOOM = 0.02;
const MAX_ZOOM = 32;

const pane = useTemplateRef("pane");
const content = useTemplateRef("content");
const size = useElementSize(pane);

// the camera, and the scale it shows, which the marks read
let camera: PanzoomObject | null = null;
const zoom = shallowRef(1);
// the content's middle sits on the pane's middle, so a zoom about its own middle keeps it there and Fit is one scale
const centered = computed(() => ({
  width: `${width}px`,
  height: `${height}px`,
  left: `calc(50% - ${width / 2}px)`,
  top: `calc(50% - ${height / 2}px)`,
}));

/** The zoom that fits the content to the pane, never past its own size. */
const fitted = computed(() =>
  width ? Math.min(1, (size.width.value - 32) / width, (size.height.value - 32) / height) : 1,
);

/** Shows the content whole, in the middle, and keeps it so until the user moves it. */
function showFitted(): void {
  camera?.zoom(Math.max(MIN_ZOOM, fitted.value), { animate: false, force: true });
  camera?.pan(0, 0, { animate: false, force: true });
  fit.value = true;
}

/** Zoom to a level within the limits, about the middle, which ends the fit. */
function zoomTo(level: number): void {
  camera?.zoom(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, level)), { animate: false });
  fit.value = false;
}

// Ctrl+wheel zooms around the pointer; the wheel alone pans, as a scrolled floor would
function onWheel(e: WheelEvent): void {
  e.preventDefault();
  if (e.ctrlKey) camera?.zoomWithWheel(e, { step: 0.25 });
  else camera?.pan(-e.deltaX / zoom.value, -e.deltaY / zoom.value, { relative: true, animate: false });
  fit.value = false;
}

// a drag's end is a click the content must not see, or a drag would select the widget under the pointer
let dragged = false;
function onClick(e: MouseEvent): void {
  if (dragged) e.stopPropagation();
  dragged = false;
}

onMounted(() => {
  if (!content.value) return;
  camera = Panzoom(content.value, { canvas: true, minScale: MIN_ZOOM, maxScale: MAX_ZOOM, cursor: "grab" });
  content.value.addEventListener("panzoomchange", (e: Event) => {
    zoom.value = (e as CustomEvent<{ scale: number }>).detail.scale;
  });
  content.value.addEventListener("panzoomstart", () => (dragged = false));
  content.value.addEventListener("panzoompan", (e: Event) => {
    if ((e as CustomEvent<{ originalEvent?: Event }>).detail.originalEvent) {
      dragged = true;
      fit.value = false;
    }
  });
  showFitted();
});
onScopeDispose(() => camera?.destroy());

// a fitted content follows the pane and its own size
watch([fitted, () => width, () => height], () => fit.value && showFitted());

defineExpose({ zoom });
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-(--bg-0)">
    <div
      ref="pane"
      class="relative min-h-0 flex-1 overflow-hidden"
      style="background: repeating-conic-gradient(var(--bg-0) 0 25%, var(--bg-4) 0 50%) 0 0 / 16px 16px"
      @wheel="onWheel"
      @click.capture="onClick"
    >
      <div ref="content" class="absolute" :style="centered">
        <slot />
      </div>
    </div>
    <div
      class="flex h-7 shrink-0 items-center gap-1.5 border-t border-default px-3.5 text-12 whitespace-nowrap text-dimmed tabular-nums"
    >
      <slot name="toolbar" />
      <span class="grow" />
      <UButton label="Fit" variant="ghost" size="xs" :disabled="!width" @click="showFitted" />
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
