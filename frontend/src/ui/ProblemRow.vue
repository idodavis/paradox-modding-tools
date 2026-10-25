<!-- One problem: its message, its severity and its line and column. -->
<script setup lang="ts">
import { computed } from "vue";

import type { Code } from "@services/lsp/models";

import { diagnosticMessage } from "../lib/format";

const { code, facts } = defineProps<{
  code: Code;
  facts: Record<string, string | undefined>;
  line: number;
  column: number;
}>();
defineEmits<{ open: [] }>();

/** The icon and the color of each severity Go sends: 1 error, 2 warning, 3 information, 4 hint. */
const SEVERITIES: Record<number, { icon: string; color: string }> = {
  1: { icon: "i-lucide-circle-x", color: "text-error" },
  2: { icon: "i-lucide-triangle-alert", color: "text-warning" },
  3: { icon: "i-lucide-info", color: "text-info" },
  4: { icon: "i-lucide-lightbulb", color: "text-dimmed" },
};
const severity = computed(() => SEVERITIES[code.severity] ?? SEVERITIES[1]);
</script>

<template>
  <button
    type="button"
    class="flex h-6 w-full items-center gap-2 px-3 text-start text-12 whitespace-nowrap text-toned hover:bg-accented"
    @click="$emit('open')"
  >
    <UIcon :name="severity?.icon" class="size-3.5 shrink-0" :class="severity?.color" />
    <span class="min-w-0 grow truncate">{{ diagnosticMessage(code.message, facts) }}</span>
    <span class="shrink-0 font-mono text-11 text-dimmed">{{ code.id }}</span>
    <span class="shrink-0 font-mono text-11 text-dimmed">{{ line }}:{{ column }}</span>
  </button>
</template>
