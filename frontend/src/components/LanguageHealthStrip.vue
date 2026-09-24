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

const { health, scanning, scanPct, scanMsg, lastScanned, scanLabel, rescan } = useLanguageHealth(
  () => props.workspaceId,
);

const scanColor = computed(() => (health.value?.cacheStale || !health.value?.scannedAt ? "warning" : "primary"));

const guideOpen = ref(false);

const effects = computed(() => (health.value?.dumps ?? []).reduce((sum, dump) => sum + dump.effects, 0));
const unexplained = computed(() =>
  Object.values(health.value?.drift ?? {}).reduce((sum, tallies) => sum + (tallies?.length ?? 0), 0),
);

// one chip for the type system, quiet in the normal case: dumps read and everything explained
const docsChip = computed(() => {
  if (!health.value?.ready) return null;
  if (!effects.value) return { label: "No script_docs", color: "text-warning", icon: "i-lucide-triangle-alert" };
  if (unexplained.value) return { label: `${unexplained.value} unexplained`, color: "text-muted", icon: "i-lucide-circle-help" };
  return null;
});
</script>

<template>
  <div v-if="health" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
    <UButton
      :label="scanLabel"
      icon="i-lucide-refresh-cw"
      size="sm"
      :color="scanColor"
      :loading="scanning"
      @click.stop="rescan"
    />
    <span :class="health.installOk ? 'text-success' : 'text-error'">
      {{ health.installOk ? "Install OK" : "Install missing" }}
    </span>
    <span v-if="health.gameVersion">
      {{ health.gameVersion }}<template v-if="health.cacheStale"> (cache stale)</template>
    </span>
    <span v-if="!health.ready">index pending</span>
    <span v-if="health.watchError" class="text-warning" :title="health.watchError">watch degraded</span>
    <span v-if="!scanning && lastScanned">Scanned {{ lastScanned }}</span>
    <div v-if="scanning" class="flex min-w-40 max-w-64 flex-1 items-center gap-2">
      <UProgress :model-value="scanPct" :max="100" size="xs" class="min-w-24 flex-1" />
      <span class="shrink-0 text-xs text-muted">{{ scanMsg }}</span>
    </div>
    <button
      v-if="docsChip"
      type="button"
      class="flex items-center gap-1 underline decoration-dotted underline-offset-2"
      :class="docsChip.color"
      @click.stop="guideOpen = true"
    >
      <UIcon :name="docsChip.icon" class="size-3.5" />
      {{ docsChip.label }}
    </button>
    <ScriptDocsGuide v-model:open="guideOpen" :dumps="health.dumps ?? []" :drift="health.drift" />
  </div>
</template>
