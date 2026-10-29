<!--
  The frame of a detail in Health: a fixed head with an icon, a title on up to two lines, a pill and a line under it,
  and buttons, over a body of sections that scrolls.
-->
<script setup lang="ts">
import type { Level } from "../../lib/format";

const {
  icon,
  label,
  color = undefined,
} = defineProps<{
  /** The icon before the title. */
  icon: string;
  /** The words in the pill under the title, such as the finding's level. */
  label: string;
  /** The color of the icon and the pill. Unset, both are neutral. */
  color?: Level["color"];
}>();
</script>

<template>
  <section class="flex h-full min-w-0 grow flex-col bg-default">
    <div class="flex shrink-0 items-start gap-2.5 border-b border-default px-4.5 pt-3.5 pb-3">
      <UIcon
        :name="icon"
        class="mt-0.75 size-4 shrink-0"
        :class="{ 'text-dimmed': !color }"
        :style="color && { color: `var(--ui-${color})` }"
      />
      <span class="flex min-w-0 grow flex-col gap-1.75">
        <span class="line-clamp-2 text-13 leading-5 font-semibold wrap-anywhere text-highlighted">
          <slot name="title" />
        </span>
        <span class="flex flex-wrap items-center gap-x-2 gap-y-1 text-12 text-dimmed">
          <UBadge :label="label" :color="color ?? 'neutral'" class="shrink-0 rounded-full font-semibold">
            <template #leading><span class="size-1.5 rounded-full bg-current" /></template>
          </UBadge>
          <slot name="line" />
        </span>
      </span>
      <!-- the buttons wrap under each other when the pane is narrow -->
      <span class="flex shrink-0 flex-wrap justify-end gap-1.5"><slot name="buttons" /></span>
    </div>
    <div class="flex min-h-0 grow flex-col overflow-auto"><slot /></div>
  </section>
</template>
