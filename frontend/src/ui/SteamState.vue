<script setup lang="ts">
/**
 * A mod's state on Steam in one line, from its summary alone (no compare): "v2.3.1, on Steam", or Not published.
 * The Publish rail and the Settings list show it.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { summaryQuery } from "../queries";

const { workspaceId, modId } = defineProps<{ workspaceId: string; modId: string }>();
const { data: summary } = useQuery(() => summaryQuery({ workspaceId, modId }));

const state = computed(() => {
  if (!summary.value) return "";
  const version = summary.value.steamVersion;
  return summary.value.remoteFileId ? (version ? `v${version}, on Steam` : "On Steam") : "Not published";
});
</script>

<template>
  <span class="truncate text-11 text-dimmed">{{ state }}</span>
</template>
