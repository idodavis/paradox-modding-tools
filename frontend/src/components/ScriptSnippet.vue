<script setup lang="ts">
/**
 * Read-only code (lib/code.ts): the boards' `.code`, its rows numbered by the file's lines; inline, one line in the
 * code's colors (a property's value).
 */
import { computed } from "vue";
import { codeLines } from "../lib/code";

const { text, name, fromLine = 0, inline = false } = defineProps<{
  text: string;
  /** The file's name or path, whose extension picks the grammar. */
  name: string;
  /** The 0-based file line `text` starts at. */
  fromLine?: number;
  inline?: boolean;
}>();

const lines = computed(() => codeLines(text, name));
</script>

<template>
  <span v-if="inline" class="font-mono text-(--fg-1)">
    <span v-for="(token, i) in lines[0]" :key="i" :style="{ color: token.color, fontStyle: token.italic ? 'italic' : undefined }">{{ token.content }}</span>
  </span>
  <div v-else class="grid grid-cols-[auto_minmax(0,1fr)] overflow-x-auto font-mono text-(--fg-1) text-code whitespace-pre">
    <template v-for="(line, n) in lines" :key="n">
      <span class="min-w-8 pe-4 text-end text-(--fg-3) select-none">{{ fromLine + n + 1 }}</span>
      <span>
        <span v-for="(token, i) in line" :key="i" :style="{ color: token.color, fontStyle: token.italic ? 'italic' : undefined }">{{ token.content }}</span>
      </span>
    </template>
  </div>
</template>
