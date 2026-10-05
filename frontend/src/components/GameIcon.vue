<script setup lang="ts">
/**
 * Compact official game icon; hidden when no asset exists for the id.
 */
import { computed } from "vue";
import iconCk3 from "@assets/Icon_CK3.png?url";
import iconEu5 from "@assets/Icon_EUV.png?url";
import iconVic3 from "@assets/Icon_Vic3.png?url";

const props = withDefaults(
  defineProps<{ gameId: string; size?: "sm" | "md" }>(),
  { size: "sm" },
);

const SRC: Record<string, string> = {
  ck3: iconCk3,
  eu5: iconEu5,
  vic3: iconVic3,
};

const src = computed(() => SRC[props.gameId]);
const sizeClass = computed(() =>
  props.size === "md" ? "size-5" : "size-4",
);
</script>

<template>
  <img
    v-if="src"
    :src="src"
    :alt="gameId"
    :class="['shrink-0 rounded-sm object-contain', sizeClass]"
    data-art
  />
</template>
