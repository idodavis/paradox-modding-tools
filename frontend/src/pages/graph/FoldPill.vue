<!--
  A dashed pill on the canvas: a fold of more nodes or a loop's marker, with an
  icon and a short label, and an arrow where a click opens it.
-->
<script setup lang="ts">
import { NODE_STATE } from "./layout";

const {
  icon,
  label,
  mono = false,
  opens = true,
  title = undefined,
} = defineProps<{
  icon: string;
  label: string;
  title?: string;
  /** Whether the label is a name in the game's script. */
  mono?: boolean;
  /** Whether a click opens the pill, which the arrow says. */
  opens?: boolean;
}>();
</script>

<template>
  <div
    :class="NODE_STATE"
    :title="title"
    class="flex h-6.5 items-center gap-1.5 rounded-full border border-dashed border-accented px-2.5 text-12 whitespace-nowrap text-dimmed"
  >
    <UIcon :name="icon" class="size-3 shrink-0" />
    <span class="truncate" :class="mono && 'font-mono text-11'">{{ label }}</span>
    <UIcon v-if="opens" name="i-lucide-chevron-right" class="ms-auto size-3 shrink-0" />
  </div>
</template>
