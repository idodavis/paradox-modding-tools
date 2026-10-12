<script setup lang="ts">
/**
 * An image tab (Stage 10's drawing): the image on the theme's checkerboard, fit to the pane until zoomed (Fit, 100%,
 * − and +, Ctrl+wheel around the pointer), and a line with its size, format, mip levels and file size. Go's image
 * route serves it, a DDS or TGA decoded there; a format it does not decode, or a file too large, says so.
 */
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { useElementSize } from "@vueuse/core";
import { bytes } from "../../lib/format";
import { imageQuery } from "../../queries";

const { path } = defineProps<{ path: string }>();
const { data: image } = useQuery(() => imageQuery(path));
const pane = useTemplateRef("pane");
const size = useElementSize(pane);

// the pixels as drawn (a DDS past 4096 a side is drawn at a smaller mip level), and a load that failed
const drawn = shallowRef({ width: 0, height: 0 });
const failed = shallowRef(false);
watch(
  () => image.value?.src,
  () => (failed.value = false),
);
function loaded(e: Event): void {
  const img = e.target as HTMLImageElement;
  drawn.value = { width: img.naturalWidth, height: img.naturalHeight };
}

// the zoom; none fits the image to the pane, never past its own size
const zoom = shallowRef<number>();
const fit = computed(() =>
  drawn.value.width
    ? Math.min(1, (size.width.value - 32) / drawn.value.width, (size.height.value - 32) / drawn.value.height)
    : 1,
);
const scale = computed(() => zoom.value ?? fit.value);
const zoomTo = (next: number) => (zoom.value = Math.min(32, Math.max(0.02, next)));
// Ctrl+wheel zooms around the pointer: the point under it stays there
function onWheel(e: WheelEvent): void {
  const el = pane.value;
  if (!e.ctrlKey || !el) return;
  e.preventDefault();
  const box = el.getBoundingClientRect();
  const x = e.clientX - box.left;
  const y = e.clientY - box.top;
  const before = scale.value;
  const ratio = zoomTo(before * (e.deltaY < 0 ? 1.25 : 0.8)) / before;
  void nextTick(() => el.scrollTo((el.scrollLeft + x) * ratio - x, (el.scrollTop + y) * ratio - y));
}

const facts = computed(() => image.value?.facts);
const shown = computed(() => image.value && !facts.value?.reject && !failed.value);
const line = computed(() => {
  const info = facts.value?.info;
  const width = info?.width || drawn.value.width;
  const height = info?.height || drawn.value.height;
  return [
    width && `${width} × ${height}`,
    facts.value?.label,
    (info?.mips ?? 0) > 1 && `${info?.mips} mip levels`,
    facts.value && bytes(facts.value.bytes),
    drawn.value.width && width !== drawn.value.width && `shown at ${drawn.value.width} × ${drawn.value.height}`,
  ]
    .filter(Boolean)
    .join(" · ");
});
const why = computed(
  () => facts.value?.reject || (failed.value ? `${facts.value?.label ?? "This image"} is not shown` : ""),
);
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
        <img
          v-if="shown"
          :src="image?.src"
          alt=""
          class="max-w-none"
          :class="{ invisible: !drawn.width }"
          :style="{ width: `${drawn.width * scale}px`, imageRendering: scale > 1 ? 'pixelated' : undefined }"
          @load="loaded"
          @error="failed = true"
        />
      </div>
      <p v-if="why" class="absolute inset-x-0 top-10 text-center text-12 text-dimmed">{{ why }}</p>
    </div>
    <div
      class="flex h-7 shrink-0 items-center gap-1.5 border-t border-default px-3.5 text-12 whitespace-nowrap text-dimmed tabular-nums"
    >
      <span class="truncate">{{ line }}</span>
      <span class="grow" />
      <UButton label="Fit" variant="ghost" size="xs" :disabled="!shown" @click="zoom = undefined" />
      <UButton label="100%" variant="ghost" size="xs" :disabled="!shown" @click="zoom = 1" />
      <UButton
        icon="i-lucide-minus"
        variant="ghost"
        size="xs"
        square
        aria-label="Zoom out"
        :disabled="!shown"
        @click="zoomTo(scale * 0.8)"
      />
      <UButton
        icon="i-lucide-plus"
        variant="ghost"
        size="xs"
        square
        aria-label="Zoom in"
        :disabled="!shown"
        @click="zoomTo(scale * 1.25)"
      />
    </div>
  </div>
</template>
