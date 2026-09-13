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

/**
 * One chip for the type system. It stays quiet when script_docs is live and
 * current, because that is the normal case and the strip is already dense.
 */
const schemaChip = computed(() => {
  const s = health.value?.schema;
  if (!s) return null;
  if (s.source === "missing") {
    return {
      label: "No script_docs",
      color: "text-warning",
      icon: "i-lucide-triangle-alert",
    };
  }
  if (s.stale) {
    return {
      label: "script_docs outdated",
      color: "text-warning",
      icon: "i-lucide-clock",
    };
  }
  if (s.source === "archive") {
    return {
      label: "script_docs from archive",
      color: "text-muted",
      icon: "i-lucide-archive",
    };
  }
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
    <span>{{ health.scriptDocsEffects }} effects</span>
    <span v-if="health.indexReady">{{ health.defCount }} defs</span>
    <span v-if="health.watchError" class="text-warning" :title="health.watchError">watch degraded</span>
    <span v-else>index pending</span>
    <span v-if="!scanning && lastScanned">Scanned {{ lastScanned }}</span>
    <div v-if="scanning" class="flex min-w-40 max-w-64 flex-1 items-center gap-2">
      <UProgress :model-value="scanPct" :max="100" size="xs" class="min-w-24 flex-1" />
      <span class="shrink-0 text-xs text-muted">{{ scanMsg }}</span>
    </div>
    <button
      v-if="schemaChip"
      type="button"
      class="flex items-center gap-1 underline decoration-dotted underline-offset-2"
      :class="schemaChip.color"
      @click.stop="guideOpen = true"
    >
      <UIcon :name="schemaChip.icon" class="size-3.5" />
      {{ schemaChip.label }}
    </button>
    <ScriptDocsGuide v-if="health.schema" v-model:open="guideOpen" :schema="health.schema" />
  </div>
</template>
