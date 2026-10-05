<script setup lang="ts">
/**
 * PMT brand mark used in the Vue app.
 */
import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
import logoText from "@assets/PMT-Logo-Text.svg?url";

const { iconHeight = 30, textHeight = 40 } = defineProps<{ iconHeight?: number; textHeight?: number }>();
</script>

<template>
  <div class="inline-flex items-center" data-art>
    <span class="inline-block" :style="{
      width: `${iconHeight * 2}px`,
      height: `${iconHeight}px`,
      backgroundColor: 'var(--accent)',
      maskImage: `url(${logoIcon})`,
      maskRepeat: 'no-repeat',
      maskPosition: 'center',
      maskSize: 'contain',
    }" />
    <span class="inline-block" :style="{
      width: `${textHeight * 3}px`,
      height: `${textHeight}px`,
      backgroundColor: 'var(--link)',
      maskImage: `url(${logoText})`,
      maskRepeat: 'no-repeat',
      maskPosition: '0% center',
      maskSize: 'contain',
      marginLeft: `${-1.0 * iconHeight}px`,
    }" />
  </div>
</template>
