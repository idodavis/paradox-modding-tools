<!--
  The one picker of the Visual view's panel: the current choice with its name and place, which opens a search and a
  grid of every choice, for a widget's pictures and a coat layer's texture and colors alike.
-->
<script setup lang="ts">
import { refDebounced } from "@vueuse/core";
import { computed, shallowRef } from "vue";

import { matchQuery } from "../../../lib/format";
import { fileUrl } from "../../../queries";

/** One choice: its value, its name, and the image file or the color that shows it. */
export type GalleryChoice = { value: string; label: string; path?: string; color?: string };

const {
  choices,
  value,
  title,
  shape,
  isPicked = false,
  disabled = false,
} = defineProps<{
  choices: readonly GalleryChoice[];
  /** The value of the current choice. */
  value: string;
  /** What is being chosen, in words. */
  title: string;
  /** How a choice is shown: a wide picture, a square texture or a color swatch. */
  shape: "wide" | "square" | "swatch";
  /** Whether the current choice was picked here rather than written, which Back to the default undoes. */
  isPicked?: boolean;
  /** Whether the choice cannot be changed, which shows the current choice alone. */
  disabled?: boolean;
}>();
const emit = defineEmits<{ pick: [value: string]; reset: [] }>();

defineSlots<{
  /** A choice's tile in place of its image or swatch, such as a texture recolored. */
  tile?: (props: { choice: GalleryChoice }) => unknown;
  /** A last cell of the grid, such as a choice of a custom color. */
  extra?: () => unknown;
}>();

// the most tiles drawn at once; a search finds the rest
const SHOWN = 60;
const SIZES = {
  wide: { preview: "h-14 w-25", tile: "aspect-video", columns: "grid-cols-4" },
  square: { preview: "size-14", tile: "aspect-square", columns: "grid-cols-5" },
  swatch: { preview: "size-9", tile: "h-7", columns: "grid-cols-6" },
};

const isOpen = shallowRef(false);
const search = shallowRef("");
const query = refDebounced(search, 150);
const current = computed(() => choices.find((choice) => choice.value === value));
const matches = computed(() => choices.filter((choice) => matchQuery(query.value, choice.label)));
const caption = computed(() => {
  const index = choices.findIndex((choice) => choice.value === value);
  return index < 0 ? title : `${title}, ${index + 1} of ${choices.length}`;
});
</script>

<template>
  <div class="flex flex-col">
    <div class="flex items-center gap-2">
      <button
        type="button"
        class="-m-1 flex min-w-0 grow items-center gap-3 rounded-md p-1 text-start enabled:hover:bg-elevated"
        :disabled="disabled"
        :aria-expanded="isOpen && !disabled"
        @click="isOpen = !isOpen"
      >
        <span
          class="shrink-0 overflow-hidden rounded-md bg-accented ring-1 ring-default"
          :class="SIZES[shape].preview"
          :style="{ background: current?.color }"
        >
          <slot v-if="current && $slots.tile" name="tile" :choice="current" />
          <img v-else-if="current?.path" :src="fileUrl(current.path)" alt="" class="size-full object-cover" />
        </span>
        <span class="flex min-w-0 flex-col gap-0.5">
          <span class="truncate text-13 font-semibold text-highlighted">{{ current?.label ?? (value || "None") }}</span>
          <span class="text-12 text-dimmed">{{ caption }}</span>
        </span>
        <UIcon
          v-if="!disabled"
          name="i-lucide-chevron-down"
          class="ms-auto size-4 shrink-0 text-dimmed transition-transform"
          :class="isOpen && 'rotate-180'"
        />
      </button>
      <UButton
        v-if="isPicked && !disabled"
        label="Back to the default"
        icon="i-lucide-rotate-ccw"
        color="neutral"
        variant="outline"
        size="xs"
        class="shrink-0"
        @click="emit('reset')"
      />
    </div>
    <UCollapsible :open="isOpen && !disabled">
      <template #content>
        <div class="flex flex-col gap-2.5 pt-3">
          <UInput
            v-if="choices.length > 8"
            v-model="search"
            icon="i-lucide-search"
            size="sm"
            :placeholder="`Find one of ${choices.length}`"
          />
          <div class="grid max-h-72 gap-2 overflow-auto p-0.5" :class="SIZES[shape].columns">
            <button
              v-for="choice in matches.slice(0, SHOWN)"
              :key="choice.value"
              type="button"
              class="flex min-w-0 flex-col gap-1 text-start text-11 text-toned"
              :class="choice.value === value && 'text-highlighted'"
              :title="choice.label"
              @click="emit('pick', choice.value)"
            >
              <span
                class="block w-full overflow-hidden rounded-md bg-accented"
                :class="[SIZES[shape].tile, choice.value === value ? 'ring-2 ring-primary' : 'ring-1 ring-default']"
                :style="{ background: choice.color }"
              >
                <slot name="tile" :choice="choice">
                  <img
                    v-if="choice.path"
                    :src="fileUrl(choice.path)"
                    alt=""
                    loading="lazy"
                    class="size-full object-cover"
                  />
                </slot>
              </span>
              <span class="truncate">{{ choice.label }}</span>
            </button>
            <slot name="extra" />
          </div>
          <span v-if="matches.length > SHOWN" class="text-11 text-dimmed">
            {{ matches.length - SHOWN }} more: search to find them.
          </span>
        </div>
      </template>
    </UCollapsible>
  </div>
</template>
