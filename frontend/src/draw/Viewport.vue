<script setup lang="ts">
/**
 * A pane's floor: its content, of a given size in CSS pixels, on the theme's checkerboard, under a camera (Panzoom):
 * dragged anywhere to pan, Ctrl+wheel to zoom around the pointer, the wheel alone to pan. Fit shows the content's focus
 * (a window on its screen), else all of it, and keeps it shown until the user moves the camera. A bar under it holds
 * the pane's status, then the zoom and Fit. The content is scaled, not resized, so text stays crisp.
 */
import { computed, onMounted, onScopeDispose, shallowRef, useTemplateRef, watch } from "vue";
import Panzoom, { type PanzoomObject } from "@panzoom/panzoom";
import { useDebounceFn, useElementSize } from "@vueuse/core";

const {
  width,
  height,
  focus = null,
  fitLimit = 1,
} = defineProps<{
  width: number;
  height: number;
  /** The part of the content Fit shows, in its pixels; all of it where none. */
  focus?: DOMRect | null;
  /** The largest zoom Fit shows the content at: a window its own size at most, a small picture larger. */
  fitLimit?: number;
}>();

const emit = defineEmits<{ /** A click on the floor around the content. */ floor: [] }>();

const MIN_ZOOM = 0.02;
const MAX_ZOOM = 32;

const pane = useTemplateRef("pane");
const content = useTemplateRef("content");
const paneSize = useElementSize(pane);

// the camera, and the scale it shows, which the marks read
let camera: PanzoomObject | null = null;
const zoom = shallowRef(1);
// the content's middle sits on the pane's middle, so a zoom about its own middle keeps it there
const contentStyle = computed(() => ({
  width: `${width}px`,
  height: `${height}px`,
  left: `calc(50% - ${width / 2}px)`,
  top: `calc(50% - ${height / 2}px)`,
}));
// the box the camera keeps fitted until the user moves it: undefined while it fits the focus, or all of the content
// where there is none; null once moved
const fitBox = shallowRef<DOMRect | null | undefined>(undefined);

/** Shows a box of the content whole in the middle of the pane, never past the fit limit, and keeps it so until the
 * user moves the camera: the zoom that fits it, and the pan that puts its middle on the content's. Without a box it
 * fits the focus, else all of the content, as they are when it fits. */
function fitTo(box?: DOMRect): void {
  fitBox.value = box;
  const area = box ?? focus ?? new DOMRect(0, 0, width, height);
  if (!camera || !area.width || !area.height) return;
  const scale = Math.min(
    fitLimit,
    (paneSize.width.value - 32) / area.width,
    (paneSize.height.value - 32) / area.height,
  );
  camera.zoom(scale, { animate: false });
  camera.pan(width / 2 - (area.x + area.width / 2), height / 2 - (area.y + area.height / 2), { animate: false });
}

/** Zooms to a level about the middle, which ends the fit; the camera keeps it within its limits. */
function zoomTo(level: number): void {
  camera?.zoom(level, { animate: false });
  fitBox.value = null;
}

/** Zooms around the pointer with Ctrl and the wheel; the wheel alone pans, as a scrolled floor would. */
function onWheel(event: WheelEvent): void {
  event.preventDefault();
  if (event.ctrlKey) camera?.zoomWithWheel(event, { step: 0.25 });
  else camera?.pan(-event.deltaX / zoom.value, -event.deltaY / zoom.value, { relative: true, animate: false });
  fitBox.value = null;
}

/** Pans the camera so a box on the screen (a selected widget's) is in the middle of the pane, when it is not in
 * view; the fit ends. */
function reveal(box: DOMRect): void {
  const room = pane.value?.getBoundingClientRect();
  if (!camera || !room) return;
  const isInView = box.left >= room.left && box.right <= room.right && box.top >= room.top && box.bottom <= room.bottom;
  if (isInView) return;
  const across = room.left + room.width / 2 - (box.left + box.width / 2);
  const down = room.top + room.height / 2 - (box.top + box.height / 2);
  camera.pan(across / zoom.value, down / zoom.value, { relative: true, animate: false });
  fitBox.value = null;
}

// the content is a GPU layer of its own, so the camera moves it without painting it again; a layer keeps the scale
// it was painted at, so a moment after a zoom ends it leaves its layer for a frame and is painted again, sharp
const sharpen = useDebounceFn(() => {
  const layer = content.value;
  if (!layer) return;
  layer.style.willChange = "auto";
  requestAnimationFrame(() => requestAnimationFrame(() => layer.style.removeProperty("will-change")));
}, 250);

// a drag's end is a click the content must not see, or a drag would select the widget under the pointer; a press
// that moves the camera a few pixels is still a click, and one on the floor around the content is the floor's
let hasDragged = false;
let panStart = { x: 0, y: 0 };
function onClick(event: MouseEvent): void {
  if (hasDragged) event.stopPropagation();
  else if (event.target === pane.value) emit("floor");
  hasDragged = false;
}

onMounted(() => {
  if (!content.value) return;
  camera = Panzoom(content.value, { canvas: true, minScale: MIN_ZOOM, maxScale: MAX_ZOOM, cursor: "grab" });
  content.value.addEventListener("panzoomchange", (event: Event) => {
    const scale = (event as CustomEvent<{ scale: number }>).detail.scale;
    if (scale !== zoom.value) void sharpen();
    zoom.value = scale;
  });
  content.value.addEventListener("panzoomstart", (event: Event) => {
    panStart = (event as CustomEvent<{ x: number; y: number }>).detail;
    hasDragged = false;
  });
  content.value.addEventListener("panzoompan", (event: Event) => {
    const { x, y, originalEvent } = (event as CustomEvent<{ x: number; y: number; originalEvent?: Event }>).detail;
    if (!originalEvent || Math.hypot(x - panStart.x, y - panStart.y) * zoom.value < 4) return;
    hasDragged = true;
    fitBox.value = null;
  });
  fitTo();
});
onScopeDispose(() => camera?.destroy());

// a fitted camera follows the pane, the content's size and its focus
watch(
  [paneSize.width, paneSize.height, () => width, () => height, () => focus],
  () => fitBox.value !== null && fitTo(fitBox.value),
);

defineExpose({ zoom, reveal, fitTo });
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
      <div ref="content" class="absolute will-change-transform" :style="contentStyle">
        <slot />
      </div>
    </div>
    <div
      class="flex h-7 shrink-0 items-center gap-1 border-t border-default bg-muted ps-2 pe-1.5 text-12 whitespace-nowrap text-dimmed tabular-nums"
    >
      <slot name="toolbar" />
      <span class="grow" />
      <UButton
        icon="i-lucide-minus"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        aria-label="Zoom out"
        :disabled="!width"
        @click="zoomTo(zoom * 0.8)"
      />
      <UTooltip text="Back to 100%">
        <UButton
          :label="`${Math.round(zoom * 100)}%`"
          variant="ghost"
          color="neutral"
          size="xs"
          class="min-w-11 justify-center"
          :disabled="!width"
          @click="zoomTo(1)"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-plus"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        aria-label="Zoom in"
        :disabled="!width"
        @click="zoomTo(zoom * 1.25)"
      />
      <USeparator orientation="vertical" class="mx-1 h-3.5" />
      <UButton
        label="Fit"
        icon="i-lucide-scan"
        :variant="fitBox !== null ? 'soft' : 'ghost'"
        :color="fitBox !== null ? 'primary' : 'neutral'"
        size="xs"
        :disabled="!width"
        @click="fitTo()"
      />
    </div>
  </div>
</template>
