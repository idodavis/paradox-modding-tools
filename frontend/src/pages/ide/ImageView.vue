<!-- An image file in a viewport with its header facts above, and a DDS's mip levels listed beside it. -->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";

import { Format } from "@internal/files/models";
import type { Place } from "@services/workspace/models";

import { canSaveAsDds } from "../../app/commands";
import { bytes } from "../../lib/format";
import { fileUrl, headerQuery } from "../../queries";
import PictureViewport from "./draw/PictureViewport.vue";
import SaveDdsModal from "./SaveDdsModal.vue";

const { place } = defineProps<{ place: Place }>();

const saveDds = useOverlay().create(SaveDdsModal);
const failed = shallowRef(false);
const level = shallowRef(0);
// Go reads headers only for the formats the games read, so a web image has no facts line.
const { data: header } = useQuery(() => ({ ...headerQuery(place.path), enabled: place.format === Format.FormatImage }));
const facts = computed(() => {
  if (!header.value) return "";
  const { width, height, format, compression, mipmaps, bytes: size } = header.value;
  const mipText = mipmaps > 1 ? `${mipmaps} mip levels` : "";
  return [`${width} × ${height}`, format.toUpperCase(), compression, mipText, bytes(size)].filter(Boolean).join(" · ");
});
const levels = computed(() => {
  const { width = 0, height = 0, mipmaps = 1 } = header.value ?? {};
  return Array.from({ length: mipmaps }, (_, index) => ({
    index,
    width: Math.max(1, width >> index),
    height: Math.max(1, height >> index),
  }));
});
const source = computed(() => toSource(level.value));
watch(
  () => place.path,
  () => (level.value = 0),
);
watch(source, () => (failed.value = false));

/** toSource returns the URL of one mip level of the image. Level 0 is the image itself. */
function toSource(index: number): string {
  return index ? `${fileUrl(place.path)}&level=${index}` : fileUrl(place.path);
}

/** snapshot returns the shown level as a PNG, decoded by the files route, or null if the fetch fails. */
async function snapshot(): Promise<Blob | null> {
  const response = await fetch(source.value);
  return response.ok ? response.blob() : null;
}
</script>

<template>
  <div class="flex min-h-0 flex-1">
    <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <p
        v-if="facts"
        class="h-7 shrink-0 truncate border-b border-default bg-muted px-3 font-mono text-11 leading-7 text-dimmed"
      >
        {{ facts }}
      </p>
      <PictureViewport
        class="min-h-0 flex-1"
        backdrop
        :snapshot="snapshot"
        :title="`${place.name.replace(/\.[^.]+$/, '')}${level ? `-level-${level}` : ''}`"
      >
        <img
          v-if="!failed"
          :key="source"
          :src="source"
          alt=""
          class="block max-w-none [image-rendering:pixelated]"
          @error="failed = true"
        />
        <template v-if="canSaveAsDds(place)" #export>
          <UButton
            label="Save as DDS"
            icon="i-lucide-file-image"
            variant="ghost"
            color="neutral"
            size="xs"
            @click="saveDds.open({ place })"
          />
        </template>
      </PictureViewport>
      <p v-if="failed" class="absolute inset-x-0 top-12 text-center text-12 text-dimmed">This image is not shown.</p>
    </div>
    <aside v-if="levels.length > 1" class="flex w-64 shrink-0 flex-col border-l border-default bg-muted">
      <div class="flex h-9 shrink-0 items-center gap-1.5 px-3 text-12 font-semibold text-toned">
        Mip levels<span class="font-normal text-dimmed">{{ levels.length }}</span>
      </div>
      <div class="flex min-h-0 flex-1 flex-col overflow-auto">
        <button
          v-for="mip in levels"
          :key="mip.index"
          type="button"
          class="flex h-10 shrink-0 items-center gap-2.5 px-3 text-12 text-toned hover:bg-elevated"
          :class="mip.index === level && 'bg-(--accent-soft) text-highlighted'"
          @click="level = mip.index"
        >
          <span
            class="flex size-7 shrink-0 items-center justify-center overflow-hidden rounded bg-(--bg-0) ring-1 ring-default"
          >
            <img
              :src="toSource(mip.index)"
              alt=""
              loading="lazy"
              class="max-h-full max-w-full [image-rendering:pixelated]"
            />
          </span>
          Level {{ mip.index }}
          <span class="ms-auto font-mono text-11 text-dimmed">{{ mip.width }} × {{ mip.height }}</span>
        </button>
      </div>
    </aside>
  </div>
</template>
