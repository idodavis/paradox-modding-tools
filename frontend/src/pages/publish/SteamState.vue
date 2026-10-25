<!-- A mod's state on Steam in the Publish rail: Go's status of its next upload, in words and in its color. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { comparisonQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";

/** Each status Go gives a mod's next upload, with the words and the color the rail shows it in. */
const STATES: Record<string, { label: string; color: string }> = {
  new: { label: "Not published yet", color: "text-dimmed" },
  unknown: { label: "On Steam", color: "text-dimmed" },
  same: { label: "Same as Steam", color: "text-success" },
  changed: { label: "Changes not on Steam", color: "text-warning" },
};
// a compare that waits its turn for Steam, or is asking it, has no status yet
const ASKING = { label: "Asking Steam…", color: "text-dimmed" };

const { modId } = defineProps<{ modId: string }>();

const workspace = useWorkspaceStore();
const { data: comparison, isLoading } = useQuery(() => ({
  ...comparisonQuery(modId),
  enabled: workspace.isOpen(),
}));
const state = computed(() =>
  comparison.value ? STATES[comparison.value.status] : isLoading.value ? ASKING : undefined,
);
</script>

<template>
  <span v-if="state" :class="state.color">{{ state.label }}</span>
</template>
