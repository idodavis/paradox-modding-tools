<!--
  An (i) that opens a popover with what a word PMT uses means. With no meaning given it reads the word's row of the
  terms table; its slot replaces the (i) as what is pressed.
-->
<script setup lang="ts">
import { computed } from "vue";

import { TERMS } from "../app/pages";

const { term, meaning = undefined } = defineProps<{ term: string; meaning?: string }>();

const text = computed(() => meaning ?? TERMS.find((row) => row.term === term)?.meaning ?? "");
</script>

<template>
  <UPopover :content="{ side: 'top' }">
    <button type="button" class="text-dimmed hover:text-highlighted" :aria-label="`What ${term} means`">
      <slot><UIcon name="i-lucide-info" class="size-3.5" /></slot>
    </button>
    <template #content>
      <div class="flex max-w-80 flex-col gap-1 p-3 text-12">
        <span class="font-semibold text-highlighted">{{ term }}</span>
        <span class="text-toned">{{ text }}</span>
      </div>
    </template>
  </UPopover>
</template>
