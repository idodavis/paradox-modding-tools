<!-- A mod's picture beside its title in bold. The game has no picture and shows a dot of its color instead. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { stateQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";
import ModAvatar from "./ModAvatar.vue";

const { origin } = defineProps<{ origin: string }>();

const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const mod = computed(() => state.value?.mods?.find((one) => one.id === origin));
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5 font-semibold text-highlighted">
    <ModAvatar v-if="mod" :mod="mod" size="3xs" />
    <span v-else class="size-2 shrink-0 rounded-full" :style="{ background: workspace.originColor(origin) }" />
    <span class="truncate">{{ workspace.originName(origin) }}</span>
  </span>
</template>
