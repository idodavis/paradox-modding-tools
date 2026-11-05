<!-- A mod's picture: its thumbnail, or its initials on its color when it has none. -->
<script setup lang="ts">
import type { AvatarProps } from "@nuxt/ui";
import type { Mod } from "@services/workspace/models";

import { fileUrl } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";

const { mod, size = "sm" } = defineProps<{ mod: Mod; size?: AvatarProps["size"] }>();

const workspace = useWorkspaceStore();
</script>

<template>
  <UAvatar
    :src="mod.thumbnail ? fileUrl(mod.thumbnail) : undefined"
    :alt="mod.descriptor.name"
    :size="size"
    class="rounded-md border border-accented"
    :ui="{ image: 'rounded-md object-cover', fallback: 'font-semibold text-inverted' }"
    :style="{ background: workspace.originColor(mod.id, mod.color) }"
  />
</template>
