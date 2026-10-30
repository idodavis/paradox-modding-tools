<!-- A localization text in its pieces: plain text as the player reads it, each data call and each key as a chip. -->
<script setup lang="ts">
import { Markup, type Segment } from "@internal/loc/models";

defineProps<{ segments: Segment[] | null | undefined }>();

/** The chip a data call and a key are drawn as, in their colors; a long one is cut, its whole text on hover. */
const CHIP =
  "inline-block h-4.5 max-w-[18em] truncate rounded-chip bg-(--bg-4) px-1.5 align-[-3px] text-[11.5px] leading-4.5";
</script>

<template>
  <template v-for="(segment, position) in segments ?? []" :key="position">
    <span v-if="segment.markup === Markup.MarkupCall" :class="CHIP" class="text-(--syn-scope)" :title="segment.text">{{
      segment.text
    }}</span>
    <span v-else-if="segment.markup === Markup.MarkupKey" :class="CHIP" class="font-mono text-(--syn-variable)">{{
      segment.text
    }}</span>
    <br v-else-if="segment.markup === Markup.MarkupBreak" />
    <span v-else-if="segment.markup === Markup.MarkupIcon" class="text-dimmed">{{ segment.text }}</span>
    <template v-else-if="segment.markup === Markup.MarkupText">{{ segment.text }}</template>
  </template>
</template>
