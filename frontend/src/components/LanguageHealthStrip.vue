<script setup lang="ts">
/**
 * Compact install/cache/index health strip; state lives in useLanguageHealth.
 */
import { computed, ref } from "vue";
import { useLanguageHealth } from "../composables/useLanguageHealth";
import ScriptDocsGuide from "./ScriptDocsGuide.vue";

const props = defineProps<{
  workspaceId: string;
}>();

const { health, scanning, scanPct, scanMsg, lastScanned } = useLanguageHealth(() => props.workspaceId);

const guideOpen = ref(false);

// whose dumps the schema read: the user's, PMT's shipped copy, or none; either may be out of date,
// and the commands that write the user's own are told with it
const dumpsChip = computed(() => {
  const live = health.value;
  if (!live?.ready || !live.dumpsFrom) return null;
  if (live.dumpsFrom === "none") return { label: "No dumps", color: "text-warning", icon: "i-lucide-triangle-alert" };
  if (live.dumpsFrom === "yours" && !live.dumpsStale) {
    return { label: "Dumps: yours ✓", color: "text-muted", icon: "i-lucide-check" };
  }
  const run = `run script_docs${live.dataTypesCommand ? ` and ${live.dataTypesCommand}` : ""}`;
  const said =
    live.dumpsFrom === "yours"
      ? `yours, older than the game · ${run} again`
      : `PMT's${live.dumpsStale ? ", may be outdated" : ""} · ${run} for yours`;
  return { label: `Dumps: ${said}`, color: "text-warning", icon: "i-lucide-circle-help" };
});
</script>

<template>
  <div v-if="health" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
    <template v-if="scanning">
      <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />
      <span class="font-medium text-default">Scanning</span>
      <span>{{ scanMsg }}</span>
      <div class="flex min-w-40 max-w-64 flex-1 items-center gap-2">
        <UProgress :model-value="scanPct" :max="100" size="xs" class="min-w-24 flex-1" />
        <span class="shrink-0 tabular-nums">{{ scanPct }}%</span>
      </div>
    </template>
    <template v-else>
      <span :class="health.installOk ? 'text-success' : 'text-error'">
        {{ health.installOk ? "Install OK" : "Install missing" }}
      </span>
      <span v-if="health.gameVersion">{{ health.gameVersion }}</span>
      <span v-if="lastScanned">Scanned {{ lastScanned }}</span>
      <span v-if="health.watchError" class="text-warning" :title="health.watchError">watch degraded</span>
    </template>
    <button
      v-if="dumpsChip && !scanning"
      type="button"
      class="flex items-center gap-1 underline decoration-dotted underline-offset-2"
      :class="dumpsChip.color"
      @click.stop="guideOpen = true"
    >
      <UIcon :name="dumpsChip.icon" class="size-3.5" />
      {{ dumpsChip.label }}
    </button>
    <ScriptDocsGuide v-model:open="guideOpen" :folders="health.dumpFolders ?? []" />
  </div>
</template>
