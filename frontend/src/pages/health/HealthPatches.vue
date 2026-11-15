<!-- Health's view of what the game's patches changed under the mods. -->
<script setup lang="ts">
import type { Finding } from "@services/health/models";
import OriginChip from "../../ui/OriginChip.vue";
import HealthDetail from "./HealthDetail.vue";
import HealthList from "./HealthList.vue";

const { findings, opened = undefined } = defineProps<{
  /** The findings of the patch question. */
  findings: Finding[];
  /** The open finding, or undefined for none. */
  opened?: Finding;
}>();
const emit = defineEmits<{ open: [finding: Finding] }>();
const filter = defineModel<string>("filter", { default: "" });
const checks = defineModel<string[]>("checks", { default: () => [] });
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col">
    <div class="flex min-h-12 shrink-0 items-center gap-2.5 px-4 pt-1.5">
      <span class="shrink-0 text-16 text-highlighted">Patches</span>
      <span class="min-w-0 text-12 text-dimmed">
        What the game changed after the version each mod is made for: names it removed or renamed, and files the mods
        copy.
      </span>
    </div>
    <HealthList
      v-model:filter="filter"
      v-model:checks="checks"
      :findings="findings"
      empty="No patch changed anything the mods use."
      :opened="opened"
      class="grow"
      @open="emit('open', $event)"
    >
      <template #trailing="{ finding }">
        <span class="flex shrink-0 gap-0.75">
          <OriginChip v-for="mod in finding.mods" :key="mod" :origin="mod" dot />
        </span>
      </template>
      <template #detail>
        <HealthDetail v-if="opened" :finding="opened" />
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </HealthList>
  </div>
</template>
