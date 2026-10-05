<script setup lang="ts">
/**
 * A pane (pmt.css .panel): bg-2 with a line-1 edge at its end; the header slot is its 32px title row
 * (.panel-h).
 */
</script>

<template>
  <section class="relative flex min-h-0 flex-col border-e border-default bg-muted">
    <header
      v-if="$slots.header"
      class="flex h-8 shrink-0 items-center gap-1.5 ps-3 pe-1.5 text-12 font-semibold whitespace-nowrap text-toned"
    >
      <slot name="header" />
    </header>
    <slot />
  </section>
</template>
