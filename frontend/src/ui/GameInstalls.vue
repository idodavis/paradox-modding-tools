<!--
  Gives its slot one game's installs: the ones PMT has recorded, and the ones found on this computer that it has not
  recorded. It draws nothing of its own.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { detectedInstallsQuery, installsQuery } from "../queries";

const { game } = defineProps<{ game: string }>();

const { data: recorded } = useQuery(() => installsQuery(game));
const { data: found } = useQuery(() => detectedInstallsQuery(game));
const detected = computed(() =>
  (found.value ?? []).filter((one) => !recorded.value?.some((install) => install.path === one.path)),
);
</script>

<template>
  <slot :recorded="recorded ?? []" :detected="detected" />
</template>
