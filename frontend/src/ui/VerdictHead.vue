<!--
  The head of a report's panel: a ring in a level's color, a kicker, the
  verdict in words and one note.
-->
<script setup lang="ts">
const {
  color,
  icon,
  kicker,
  note = "",
} = defineProps<{
  /** The Nuxt UI color of the ring, or "neutral" for none. */
  color: string;
  icon: string;
  kicker: string;
  note?: string;
}>();

/** describeRing returns the style of a ring in a Nuxt UI color. */
function describeRing(): Record<string, string> {
  const base = color === "neutral" ? "var(--ui-text-muted)" : `var(--ui-${color})`;
  return {
    color: base,
    background: `color-mix(in oklab, ${base} 16%, transparent)`,
    boxShadow: `0 0 0 4px color-mix(in oklab, ${base} 8%, transparent)`,
  };
}
</script>

<template>
  <div class="flex items-center gap-3">
    <span class="flex size-10 shrink-0 items-center justify-center rounded-full" :style="describeRing()">
      <UIcon :name="icon" class="size-5" />
    </span>
    <span class="flex min-w-0 grow flex-col">
      <span class="text-11 font-semibold text-dimmed">{{ kicker }}</span>
      <span class="line-clamp-2 text-20 leading-tight text-highlighted"><slot /></span>
      <span class="line-clamp-2 text-11 text-dimmed">{{ note }}</span>
    </span>
    <slot name="trailing" />
  </div>
</template>
