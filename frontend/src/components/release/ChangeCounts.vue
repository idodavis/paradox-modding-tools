<script setup lang="ts">
/**
 * The lines a change adds and removes, "+38 −12" (the PublishCompare board's counts): from two texts, or
 * read from two files, "" for a side that has none.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { lineCounts } from "../../lib/code";
import { textQuery } from "../../queries";

const { texts, files } = defineProps<{ texts?: [string, string]; files?: [string, string] }>();
const { data: before } = useQuery(() => textQuery(files?.[0] ?? ""));
const { data: after } = useQuery(() => textQuery(files?.[1] ?? ""));

const counts = computed(() => {
  const [old, now] = texts ?? [files?.[0] ? before.value : "", files?.[1] ? after.value : ""];
  return old === undefined || now === undefined ? undefined : lineCounts(old, now);
});
</script>

<template>
  <span v-if="counts" class="ms-auto shrink-0 font-mono text-11">
    <span v-if="counts.added" class="text-success">+{{ counts.added }}</span>
    <span v-if="counts.removed" class="ms-1 text-error">−{{ counts.removed }}</span>
  </span>
</template>
