<!-- Gives its slot one game's installs, recorded and found. It draws nothing of its own. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { detectedInstallsQuery, installsQuery } from "../queries";

const { gameId } = defineProps<{ gameId: string }>();

const { data: recorded } = useQuery(() => installsQuery(gameId));
const { data: scanned } = useQuery(() => detectedInstallsQuery(gameId));
const detected = computed(() =>
  (scanned.value ?? []).filter((candidate) => !recorded.value?.some((install) => install.path === candidate.path)),
);
</script>

<template>
  <slot :recorded="recorded ?? []" :detected="detected" />
</template>
