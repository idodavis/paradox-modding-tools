<!--
  A button that names the origins a graph draws, and a menu to check or
  uncheck each of them. The menu's slot holds more of the filter.
-->
<script lang="ts">
export const CHECKBOX_UI = {
  item: "h-6.5 items-center",
  wrapper: "flex grow items-center gap-2",
  label: "flex grow items-center gap-2 text-12",
};
</script>

<script setup lang="ts">
import { computed } from "vue";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";

const { origins } = defineProps<{ origins: string[] }>();
const drawn = defineModel<string[]>({ required: true });

const workspace = useWorkspaceStore();
const choices = computed(() =>
  origins.map((origin) => ({
    value: origin,
    disabled: drawn.value.length === 1 && drawn.value.includes(origin),
  })),
);
</script>

<template>
  <UPopover :content="{ align: 'start' }">
    <UButton
      :label="`From: ${drawn.map(workspace.originName).join(', ')}`"
      variant="outline"
      size="xs"
      color="neutral"
      :ui="{ label: 'max-w-64 truncate' }"
    />
    <template #content>
      <div class="flex w-80 flex-col gap-2 p-3.5">
        <p class="text-12 text-dimmed">Unchecked origins hide what only they define.</p>
        <UCheckboxGroup
          v-model="drawn"
          color="secondary"
          :items="choices"
          :ui="{ ...CHECKBOX_UI, fieldset: 'gap-y-0' }"
        >
          <template #label="{ item }"><OriginChip :origin="item.value" /></template>
        </UCheckboxGroup>
        <slot />
      </div>
    </template>
  </UPopover>
</template>
