<!--
  A tinted block of what the patches changed under a mod: a header with its
  count, its rows, and a line that offers the rest.
-->
<script setup lang="ts">
import { describeCount } from "../../lib/format";

const {
  tone,
  icon,
  title,
  count,
  aside = "",
  more = "",
} = defineProps<{
  /** The Nuxt UI color of the header's tint. */
  tone: string;
  icon: string;
  title: string;
  count: number;
  /** A note at the header's right end. */
  aside?: string;
  /** The words of the line that offers the rest of the rows. "" for no line. */
  more?: string;
}>();
const emit = defineEmits<{ more: [] }>();
</script>

<template>
  <section class="min-w-0 overflow-hidden rounded-lg border border-default bg-muted">
    <header
      class="flex h-8.5 items-center gap-2 px-3 text-12 font-semibold text-highlighted"
      :style="{
        background: `color-mix(in oklab, var(--ui-${tone}) 10%, var(--bg-2))`,
        boxShadow: `inset 3px 0 0 var(--ui-${tone})`,
      }"
    >
      <UIcon :name="icon" class="size-3.5 shrink-0" />
      <span class="truncate">{{ title }}</span>
      <span class="font-normal text-dimmed">{{ describeCount(count) }}</span>
      <span class="grow" />
      <span v-if="aside" class="text-11 font-normal whitespace-nowrap text-dimmed">{{ aside }}</span>
    </header>
    <slot />
    <div v-if="more" class="border-t border-default px-3 py-1.5">
      <UButton :label="more" variant="link" size="xs" class="p-0" @click="emit('more')" />
    </div>
  </section>
</template>
