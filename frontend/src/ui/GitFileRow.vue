<!-- One changed file: its status, its path, and buttons to stage, unstage, discard and open. -->
<script setup lang="ts">
import type { Change } from "@services/git/models";

import OriginChip from "./OriginChip.vue";

defineProps<{ change: Change }>();
const emit = defineEmits<{ open: []; stage: []; unstage: []; discard: [] }>();

const COLORS: Record<string, string> = {
  modified: "text-warning",
  added: "text-success",
  untracked: "text-success",
  deleted: "text-error",
  renamed: "text-info",
  conflict: "text-error",
};
</script>

<template>
  <div
    class="group flex h-7 w-full shrink-0 cursor-pointer items-center gap-2 pe-2 text-start whitespace-nowrap text-toned hover:bg-elevated"
    :class="{ 'ps-3.5': !$slots.lane }"
    :title="change.from ? `${change.from} → ${change.path}` : change.path"
  >
    <slot name="lane" />
    <OriginChip :origin="change.place.origin" dot />
    <span class="min-w-0 grow truncate">
      <span class="text-highlighted">{{ change.place.name }}</span>
      <span class="ms-1.5 text-dimmed">{{ change.place.folder }}</span>
    </span>
    <UButton
      v-if="(change.staged || change.unstaged) && change.status !== 'deleted' && change.status !== 'conflict'"
      class="hidden group-hover:flex"
      icon="i-lucide-file"
      variant="ghost"
      size="xs"
      square
      aria-label="Open the file"
      @click.stop="emit('open')"
    />
    <UButton
      v-if="change.unstaged && change.status !== 'conflict'"
      class="hidden group-hover:flex"
      icon="i-lucide-undo-2"
      color="error"
      variant="ghost"
      size="xs"
      square
      aria-label="Discard"
      @click.stop="emit('discard')"
    />
    <UButton
      v-if="change.unstaged"
      class="hidden group-hover:flex"
      icon="i-lucide-plus"
      variant="ghost"
      size="xs"
      square
      :aria-label="change.status === 'conflict' ? 'Stage, resolved' : 'Stage'"
      @click.stop="emit('stage')"
    />
    <UButton
      v-if="change.staged"
      class="hidden group-hover:flex"
      icon="i-lucide-minus"
      variant="ghost"
      size="xs"
      square
      aria-label="Unstage"
      @click.stop="emit('unstage')"
    />
    <b class="w-3.5 shrink-0 text-center font-mono font-medium uppercase" :class="COLORS[change.status]">{{
      change.status === "conflict" ? "!" : change.status.charAt(0)
    }}</b>
  </div>
</template>
