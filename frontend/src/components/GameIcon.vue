<script setup lang="ts">
/**
 * A game's icon (pmt.css .gameico): 18px, or 36px large; nothing for a game with no icon.
 */
import iconCk3 from "@assets/Icon_CK3.png?url";
import iconEu5 from "@assets/Icon_EUV.png?url";
import iconVic3 from "@assets/Icon_Vic3.png?url";

const { gameId, size = "sm" } = defineProps<{ gameId: string; size?: "sm" | "lg" }>();

const SRC: Record<string, string> = { ck3: iconCk3, eu5: iconEu5, vic3: iconVic3 };
</script>

<template>
  <img
    v-if="SRC[gameId]"
    :src="SRC[gameId]"
    :alt="gameId"
    class="shrink-0 object-contain"
    :class="size === 'lg' ? 'size-9 rounded-lg' : 'size-4.5 rounded-sm'"
    data-art
  />
</template>
