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

const unexplained = computed(() =>
  Object.values(health.value?.unread ?? {}).reduce((sum, tallies) => sum + (tallies?.length ?? 0), 0),
);

// where each dump set came from: the user's, PMT's shipped copy (with the command that writes the
// user's own), or none; EU5 publishes no version, so a shipped set may have none
const dumpsChip = computed(() => {
  const live = health.value;
  if (!live?.ready || !live.dumpsFrom) return null;
  const { scriptDocs, dataTypes } = live.dumpsFrom;
  if (scriptDocs === "none" && dataTypes === "none") {
    return { label: "No dumps", color: "text-warning", icon: "i-lucide-triangle-alert" };
  }
  const said = (from: string) => (from === "yours" ? "yours" : from === "shipped" ? `PMT's${live.dumpsVersion ? `, game ${live.dumpsVersion}` : ""}` : "none");
  const sets = scriptDocs === dataTypes ? said(scriptDocs) : `${said(scriptDocs)}; data types ${said(dataTypes)}`;
  const run = [scriptDocs !== "yours" && "script_docs", dataTypes !== "yours" && live.dataTypesCommand].filter(Boolean);
  if (!run.length) return { label: `Dumps: ${sets} ✓`, color: "text-muted", icon: "i-lucide-check" };
  return { label: `Dumps: ${sets} · run ${run.join(" and ")} for yours`, color: "text-warning", icon: "i-lucide-circle-help" };
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
      v-if="dumpsChip"
      type="button"
      class="flex items-center gap-1 underline decoration-dotted underline-offset-2"
      :class="dumpsChip.color"
      @click.stop="guideOpen = true"
    >
      <UIcon :name="dumpsChip.icon" class="size-3.5" />
      {{ dumpsChip.label }}
    </button>
    <button
      v-if="health.ready && unexplained"
      type="button"
      class="flex items-center gap-1 text-muted underline decoration-dotted underline-offset-2"
      @click.stop="guideOpen = true"
    >
      <UIcon name="i-lucide-circle-help" class="size-3.5" />
      {{ unexplained }} unexplained
    </button>
    <ScriptDocsGuide v-model:open="guideOpen" :dumps="health.dumps ?? []" :unread="health.unread" />
  </div>
</template>
