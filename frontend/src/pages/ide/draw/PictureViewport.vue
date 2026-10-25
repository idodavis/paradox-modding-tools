<!--
  A camera over a picture: drag to pan, Ctrl wheel to zoom, and Fit. A bar under it holds the zoom, a slot, and where
  asked the backdrop and an export to PNG. Its pane around the picture takes focus, so the page can listen there for
  clicks and keys off the picture too.
-->
<script lang="ts">
/** How far a press must move on the screen, in pixels, to be a drag rather than a click. */
export const DRAG = 4;
</script>

<script setup lang="ts">
import Panzoom, { type PanzoomObject } from "@panzoom/panzoom";
import { useMutation } from "@pinia/colada";
import { useDebounceFn, useResizeObserver } from "@vueuse/core";
import { Dialogs } from "@wailsio/runtime";
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef } from "vue";

import * as draw from "@services/draw/service";

import { useDrawStore } from "../../../stores/draw";

const MIN_ZOOM = 0.02;
const MAX_ZOOM = 32;
// the room left around a fitted box
const MARGIN = 32;
const CHECKS = "repeating-conic-gradient(var(--bg-0) 0 25%, var(--bg-4) 0 50%) 0 0 / 16px 16px";
// the backdrops a coat or an image can sit on, with what each paints; any other is a color
const BACKDROPS = [
  { value: "dark", label: "Dark", paint: "var(--bg-0)" },
  { value: "light", label: "Light", paint: "#e6e4df" },
  { value: "checks", label: "Checks", paint: CHECKS },
];

const {
  backdrop = false,
  snapshot = null,
  title = "picture",
} = defineProps<{
  /** Whether the picture sits on the backdrop chosen in the bar, rather than on checks. */
  backdrop?: boolean;
  /** Makes the PNG that Export writes, or null when there is nothing to export. With none, there is no Export. */
  snapshot?: (() => Promise<Blob | null>) | null;
  /** The name an exported PNG is offered under, without its extension. */
  title?: string;
}>();

const store = useDrawStore();
const toast = useToast();
const pane = useTemplateRef("pane");
const content = useTemplateRef("content");
const paint = computed(() =>
  backdrop ? (BACKDROPS.find(({ value }) => value === store.backdrop)?.paint ?? store.backdrop) : CHECKS,
);

const { mutate: exportPng, isLoading: isExporting } = useMutation({
  mutation: async () => {
    const blob = await snapshot?.();
    if (!blob) return "";
    const target = await Dialogs.SaveFile({
      Filename: `${title}.png`,
      Filters: [{ DisplayName: "PNG", Pattern: "*.png" }],
    });
    if (!target) return "";
    const bytes = new Uint8Array(await blob.arrayBuffer());
    await draw.ExportPNG(target, bytes.toBase64());
    return target;
  },
  onSuccess: (target) => target && toast.add({ title: "PNG saved", description: target, color: "success" }),
});

let camera: PanzoomObject | null = null;
const zoom = shallowRef(1);
// undefined fits the whole picture, null means the user has moved the camera
const fitBox = shallowRef<DOMRect | null | undefined>(undefined);
let isPanning = false;
let hasDragged = false;
let pressPan = { x: 0, y: 0 };

useResizeObserver([pane, content], () => {
  if (fitBox.value !== null) fit(fitBox.value);
});

// a layer keeps the scale it was painted at, so the picture leaves its layer for a frame to be painted sharp
const sharpen = useDebounceFn(() => {
  const layer = content.value;
  if (!layer) return;
  layer.style.willChange = "auto";
  requestAnimationFrame(() => requestAnimationFrame(() => layer.style.removeProperty("will-change")));
}, 250);

/**
 * Shows a box of the picture whole in the middle of the pane, no larger than it is. With no box it shows all of the
 * picture, and keeps showing it as the picture and the pane change size. A box with no size is not shown.
 */
function fit(box?: DOMRect): void {
  const room = pane.value;
  const picture = content.value;
  if (!camera || !room || !picture?.offsetWidth || !picture.offsetHeight) return;
  const area = box ?? new DOMRect(0, 0, picture.offsetWidth, picture.offsetHeight);
  if (!area.width || !area.height) return;
  const scale = Math.min(1, (room.clientWidth - MARGIN) / area.width, (room.clientHeight - MARGIN) / area.height);
  camera.zoom(scale, { animate: false });
  camera.pan(
    picture.offsetWidth / 2 - (area.x + area.width / 2),
    picture.offsetHeight / 2 - (area.y + area.height / 2),
    { animate: false },
  );
  fitBox.value = box;
}

/** Pans the camera so a box of the picture is in the middle of the pane, when it is not in view. A pan ends the fit. */
function reveal(box: DOMRect): void {
  const room = pane.value?.getBoundingClientRect();
  const origin = content.value?.getBoundingClientRect();
  if (!camera || !room || !origin) return;
  const [left, top] = [origin.left + box.left * zoom.value, origin.top + box.top * zoom.value];
  const [width, height] = [box.width * zoom.value, box.height * zoom.value];
  if (left >= room.left && left + width <= room.right && top >= room.top && top + height <= room.bottom) return;
  const across = room.left + room.width / 2 - (left + width / 2);
  const down = room.top + room.height / 2 - (top + height / 2);
  camera.pan(across / zoom.value, down / zoom.value, { relative: true, animate: false });
  fitBox.value = null;
}

/** Zooms to a level about the middle, which ends the fit. */
function applyZoom(level: number): void {
  camera?.zoom(level, { animate: false });
  fitBox.value = null;
}

/** Zooms around the pointer with Ctrl and the wheel. The wheel alone pans. */
function onWheel(event: WheelEvent): void {
  if (event.ctrlKey) camera?.zoomWithWheel(event, { step: 0.25 });
  else camera?.pan(-event.deltaX / zoom.value, -event.deltaY / zoom.value, { relative: true, animate: false });
  fitBox.value = null;
}

/** Keeps the click that ends a drag from the picture and the pane's own listeners, so a pan selects nothing. */
function onClick(event: MouseEvent): void {
  if (hasDragged) event.stopImmediatePropagation();
  hasDragged = false;
}

onMounted(() => {
  const element = content.value;
  if (!element) return;
  const panzoom = Panzoom(element, { canvas: true, minScale: MIN_ZOOM, maxScale: MAX_ZOOM, cursor: "grab" });
  camera = panzoom;
  element.addEventListener("panzoomchange", () => {
    if (panzoom.getScale() !== zoom.value) void sharpen();
    zoom.value = panzoom.getScale();
  });
  element.addEventListener("panzoomstart", () => {
    isPanning = true;
    hasDragged = false;
    pressPan = panzoom.getPan();
  });
  element.addEventListener("panzoompan", () => {
    const { x, y } = panzoom.getPan();
    if (!isPanning || Math.hypot(x - pressPan.x, y - pressPan.y) * zoom.value < DRAG) return;
    hasDragged = true;
    fitBox.value = null;
  });
  element.addEventListener("panzoomend", () => (isPanning = false));
  fit();
});
onBeforeUnmount(() => camera?.destroy());

defineExpose({ fit, reveal, pane });
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-(--bg-0)">
    <div
      ref="pane"
      tabindex="-1"
      class="relative min-h-0 flex-1 overflow-hidden outline-none"
      :style="{ background: paint }"
      @wheel.prevent="onWheel"
      @click.capture="onClick"
    >
      <div
        ref="content"
        class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 will-change-transform"
        :style="{ '--zoom': zoom }"
      >
        <slot />
      </div>
    </div>
    <div
      class="flex h-7 shrink-0 items-center gap-1 border-t border-default bg-muted ps-2 pe-1.5 text-12 whitespace-nowrap text-dimmed tabular-nums"
    >
      <slot name="toolbar" />
      <span class="grow" />
      <template v-if="backdrop">
        <UFieldGroup size="xs">
          <UButton
            v-for="option in BACKDROPS"
            :key="option.value"
            :label="option.label"
            color="neutral"
            :variant="store.backdrop === option.value ? 'soft' : 'ghost'"
            @click="store.backdrop = option.value"
          />
        </UFieldGroup>
        <UPopover>
          <UButton
            aria-label="A backdrop color"
            color="neutral"
            :variant="store.backdrop.startsWith('#') ? 'soft' : 'ghost'"
            size="xs"
            square
          >
            <span class="size-3.5 rounded-sm ring-1 ring-default" :style="{ background: paint }" />
          </UButton>
          <template #content>
            <UColorPicker
              class="p-2"
              :model-value="store.backdrop.startsWith('#') ? store.backdrop : '#5b6b4a'"
              @update:model-value="(color) => color && (store.backdrop = color)"
            />
          </template>
        </UPopover>
        <USeparator orientation="vertical" class="mx-1 h-3.5" />
      </template>
      <template v-if="snapshot">
        <UButton
          label="Export PNG"
          icon="i-lucide-download"
          variant="ghost"
          color="neutral"
          size="xs"
          :loading="isExporting"
          @click="exportPng()"
        />
        <USeparator orientation="vertical" class="mx-1 h-3.5" />
      </template>
      <UButton
        icon="i-lucide-minus"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        aria-label="Zoom out"
        @click="applyZoom(zoom * 0.8)"
      />
      <UTooltip text="Back to 100%">
        <UButton
          :label="`${Math.round(zoom * 100)}%`"
          variant="ghost"
          color="neutral"
          size="xs"
          class="min-w-11 justify-center"
          @click="applyZoom(1)"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-plus"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        aria-label="Zoom in"
        @click="applyZoom(zoom * 1.25)"
      />
      <USeparator orientation="vertical" class="mx-1 h-3.5" />
      <UButton
        label="Fit"
        icon="i-lucide-scan"
        :variant="fitBox === null ? 'ghost' : 'soft'"
        :color="fitBox === null ? 'neutral' : 'primary'"
        size="xs"
        @click="fit()"
      />
    </div>
  </div>
</template>
