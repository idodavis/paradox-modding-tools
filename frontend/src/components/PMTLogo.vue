<script setup lang="ts">
/**
 * PMT's brand (pmt.css .brand): the logo's mark, 18px in the accent, then PMT.
 */
import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
</script>

<template>
  <span class="flex shrink-0 items-center gap-2 text-13 font-semibold text-highlighted" data-art>
    <span
      class="size-4.5 bg-primary"
      :style="{ maskImage: `url(${logoIcon})`, maskRepeat: 'no-repeat', maskPosition: 'center', maskSize: 'contain' }"
    />
    PMT
  </span>
</template>
