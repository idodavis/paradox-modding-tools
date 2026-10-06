<script setup lang="ts">
/**
 * A mod's state on Steam in one line: the Publish rail's Not published yet, Same as Steam or Changes not
 * on Steam, from its compare; `brief`, the Settings list's "v2.3.1, on Steam" or Not published, from its
 * summary alone.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { compareQuery, summaryQuery } from "../../queries";

const { workspaceId, modId, brief = false } = defineProps<{ workspaceId: string; modId: string; brief?: boolean }>();
const { data: summary } = useQuery(() => ({ ...summaryQuery({ workspaceId, modId }), enabled: brief }));
const { data: compare } = useQuery(() => ({ ...compareQuery({ workspaceId, modId }), enabled: !brief }));

const state = computed((): [string, string] => {
  if (brief) {
    if (!summary.value) return ["", ""];
    const version = summary.value.steamVersion;
    return [summary.value.remoteFileId ? (version ? `v${version}, on Steam` : "On Steam") : "Not published", "text-dimmed"];
  }
  const steam = compare.value;
  if (!steam) return ["", ""];
  if (!steam.item) return ["Not published yet", "text-dimmed"];
  if (!steam.from) return ["On Steam, not compared yet", "text-dimmed"];
  return steam.changes ? ["Changes not on Steam", "text-warning"] : ["Same as Steam", "text-success"];
});
</script>

<template>
  <span class="truncate text-11" :class="state[1]">{{ state[0] }}</span>
</template>
