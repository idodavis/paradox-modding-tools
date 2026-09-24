<script setup lang="ts">
/**
 * A selected row's pane: its title, the page's badges, its file with Open in IDE, then the page's
 * own body.
 */
import { useOpenInIde } from "../composables/useOpenInIde";

const props = defineProps<{
  workspaceId: string;
  title?: string;
  file?: string;
  rel?: string;
  line?: number;
}>();

defineSlots<{
  badges(): unknown;
  default(): unknown;
  empty(): unknown;
}>();

const { openInIde } = useOpenInIde();

/** Open the header file in the workspace IDE, at its line (0-based here, 1-based in the editor). */
function open(): void {
  if (props.file) void openInIde(props.workspaceId, props.file, (props.line ?? 0) + 1);
}
</script>

<template>
  <aside
    class="flex h-full min-h-0 w-full flex-col overflow-hidden border-l border-default bg-elevated"
  >
    <div
      v-if="!title"
      class="m-3 rounded-md border border-dashed border-muted px-3 py-4 text-sm text-muted"
    >
      <slot name="empty">Select a row.</slot>
    </div>
    <template v-else>
      <div class="shrink-0 space-y-2 border-b border-default px-4 py-3">
        <div class="truncate text-lg font-semibold text-highlighted">
          {{ title }}
        </div>
        <div v-if="$slots.badges" class="flex flex-wrap items-center gap-1.5">
          <slot name="badges" />
        </div>
        <div v-if="file" class="flex items-center gap-2">
          <span class="min-w-0 flex-1 truncate font-mono text-xs text-muted" :title="file">
            {{ rel || file }}:{{ (line ?? 0) + 1 }}
          </span>
          <UButton
            label="Open in IDE"
            icon="i-lucide-file-code"
            size="xs"
            color="neutral"
            variant="outline"
            @click="open"
          />
        </div>
      </div>
      <div v-if="$slots.default" class="min-h-0 flex-1 overflow-auto p-4">
        <slot />
      </div>
    </template>
  </aside>
</template>
