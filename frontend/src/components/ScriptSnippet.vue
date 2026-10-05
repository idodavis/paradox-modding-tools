<script setup lang="ts">
/**
 * Recessed colorized snippet with an optional line gutter and hit-line mark.
 */
import { computed, ref, watch } from "vue";
import { colorizeLines, type ColorizeLang } from "../colorize";

const props = defineProps<{
  text: string;
  language: ColorizeLang;
  /** 0-based first line of `text`. */
  fromLine?: number;
  /** 0-based hit line in the source file. */
  hitLine?: number;
  gutter?: boolean;
}>();

const htmlLines = ref<string[]>([]);
const sourceLines = computed(() => (props.text ?? "").split("\n"));
const from = computed(() => props.fromLine ?? 0);

// the plain lines show until the workbench can color them; a coloring for older text is dropped
watch(
  () => [props.text, props.language] as const,
  async ([text, language]) => {
    htmlLines.value = [];
    const colored = await colorizeLines((text ?? "").split("\n"), language);
    if (text === props.text) htmlLines.value = colored;
  },
  { immediate: true },
);

/** True when this display line is the hit line. */
function isHit(i: number): boolean {
  return props.hitLine != null && from.value + i === props.hitLine;
}
</script>

<template>
  <pre
    class="my-0 overflow-x-auto rounded-md border border-accented bg-elevated px-2 py-1.5 text-xs leading-relaxed"
  ><div
      v-for="(line, i) in sourceLines"
      :key="i"
      class="flex gap-2"
      :class="isHit(i) ? 'rounded-sm bg-error/20' : ''"
    >
      <span
        v-if="gutter"
        class="w-8 shrink-0 select-none text-right tabular-nums text-muted"
      >{{ from + i + 1 }}</span>
      <span v-if="htmlLines[i]" class="min-w-0 flex-1" v-html="htmlLines[i]" />
      <span v-else class="min-w-0 flex-1">{{ line || " " }}</span>
    </div></pre>
</template>
