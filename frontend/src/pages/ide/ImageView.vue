<script setup lang="ts">
/**
 * An image tab: the image in a Viewport (fit to the pane until zoomed), and a line under it with its size, format, mip
 * levels and file size. Go's image route serves it, a DDS or TGA decoded there; a format it does not decode, or a file
 * too large, says so.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { bytes } from "../../lib/format";
import { imageQuery } from "../../queries";
import Viewport from "../../draw/Viewport.vue";

const { path } = defineProps<{ path: string }>();
const { data: image } = useQuery(() => imageQuery(path));
const viewport = useTemplateRef("viewport");

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

const facts = computed(() => image.value?.facts);
const visible = computed(() => image.value && !facts.value?.reject && !failed.value);
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
  <div class="relative flex min-h-0 flex-1 flex-col">
    <Viewport ref="viewport" :width="visible ? drawn.width : 0" :height="visible ? drawn.height : 0">
      <img
        v-if="visible"
        :src="image?.src"
        alt=""
        class="block max-w-none"
        :class="{ invisible: !drawn.width }"
        :style="{ imageRendering: (viewport?.zoom ?? 1) > 1 ? 'pixelated' : undefined }"
        @load="loaded"
        @error="failed = true"
      />
      <template #toolbar>
        <span class="truncate">{{ line }}</span>
      </template>
    </Viewport>
    <p v-if="why" class="absolute inset-x-0 top-10 text-center text-12 text-dimmed">{{ why }}</p>
  </div>
</template>
