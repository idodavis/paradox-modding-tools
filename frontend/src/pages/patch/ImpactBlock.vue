<!--
  A tinted block of what the patches changed under a mod: a header with its
  count, its first rows, and a line that shows the rest. With no rows it draws
  nothing.
-->
<script setup lang="ts" generic="Row">
import { computed, shallowRef } from "vue";

import { describeCount } from "../../lib/format";

const {
  tone,
  icon,
  title,
  rows,
  aside = "",
} = defineProps<{
  /** The Nuxt UI color of the header's tint. */
  tone: string;
  icon: string;
  title: string;
  rows: Row[];
  /** A note at the header's right end. */
  aside?: string;
}>();

/** How many rows the block lists before it offers the rest. */
const ROWS_SHOWN = 5;

const expanded = shallowRef(false);
const shown = computed(() => (expanded.value ? rows : rows.slice(0, ROWS_SHOWN)));
</script>

<template>
  <section v-if="rows.length" class="min-w-0 overflow-hidden rounded-lg border border-default bg-muted">
    <header
      class="flex h-8.5 items-center gap-2 px-3 text-12 font-semibold text-highlighted"
      :style="{
        background: `color-mix(in oklab, var(--ui-${tone}) 10%, var(--bg-2))`,
        boxShadow: `inset 3px 0 0 var(--ui-${tone})`,
      }"
    >
      <UIcon :name="icon" class="size-3.5 shrink-0" />
      <span class="truncate">{{ title }}</span>
      <span class="font-normal text-dimmed">{{ describeCount(rows.length) }}</span>
      <span class="grow" />
      <span v-if="aside" class="text-11 font-normal whitespace-nowrap text-dimmed">{{ aside }}</span>
    </header>
    <template v-for="(row, position) in shown" :key="position">
      <slot :row="row" />
    </template>
    <div v-if="rows.length > shown.length" class="border-t border-default px-3 py-1.5">
      <UButton
        :label="`${describeCount(rows.length - shown.length)} more`"
        variant="link"
        size="xs"
        class="p-0"
        @click="expanded = true"
      />
    </div>
  </section>
</template>
