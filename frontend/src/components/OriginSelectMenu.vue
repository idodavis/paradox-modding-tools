<script setup lang="ts">
/**
 * Multi-select origin menu. Default-all when items change. Vanilla may set gameId.
 */
import { watch } from "vue";

export type OriginMenuItem = {
  id: string;
  label: string;
  color: string;
  thumbnail?: string;
  gameId?: string;
};

const props = defineProps<{
  items: OriginMenuItem[];
}>();

const selected = defineModel<string[]>({ default: () => [] });

watch(
  () => props.items.map((i) => i.id).join("\0"),
  () => {
    const ids = props.items.map((i) => i.id);
    const keep = selected.value.filter((id) => ids.includes(id));
    selected.value = keep.length ? keep : ids;
  },
  { immediate: true },
);

const MENU_UI = { content: "min-w-96 w-[420px]", viewport: "max-h-96" };
</script>

<template>
  <USelectMenu
    v-model="selected"
    :items="items"
    value-key="id"
    label-key="label"
    multiple
    placeholder="Origins"
    size="md"
    class="w-72"
    :ui="MENU_UI"
  >
    <template #item-leading="{ item }">
      <GameIcon v-if="item.gameId" :game-id="item.gameId" />
      <Thumb v-else-if="item.thumbnail" :path="item.thumbnail" size="xs" />
      <span
        v-else
        class="size-2 shrink-0 rounded-full"
        :style="{ backgroundColor: item.color }"
      />
    </template>
  </USelectMenu>
</template>
