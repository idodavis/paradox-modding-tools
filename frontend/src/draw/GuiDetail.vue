<script setup lang="ts">
/**
 * The Widgets panel of a GUI file: under the file's header, where the selected widget sits (its ancestors, each a
 * click away), whether it is shown for this session, and its properties as written in the code's colors, each with
 * who wrote it: the widget, or the type or template it comes from. The selection is the pane's or the caret's.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import type { BreadcrumbItem } from "@nuxt/ui";
import { useDraw } from "../composables/useDraw";
import { useIdeRoots } from "../composables/useIdeRoots";
import { fileName } from "../lib/format";
import { propertiesQuery } from "../queries";
import { useDrawStore } from "../stores/draw";
import DetailHeader from "../ui/DetailHeader.vue";
import ScriptSnippet from "../ui/ScriptSnippet.vue";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A widget on the way to the selected one: its row of the breadcrumb and its part. */
type Crumb = BreadcrumbItem & { part: number };

const store = useDrawStore();
const { locate } = useIdeRoots(() => workspaceId);
const located = computed(() => locate(path));
const { selected, request, parts } = useDraw(
  () => workspaceId,
  () => path,
);

// the selected part and the way down to it, root first
const part = computed(() => parts.value[selected.value]);
const crumbs = computed(() => {
  const items: Crumb[] = [];
  for (let at = selected.value; at >= 0; at = parts.value[at].parent) {
    const one = parts.value[at];
    items.unshift({ label: one.name || one.key, part: at });
  }
  return items;
});

// whether the selected part itself is hidden for the session (one under a hidden part is drawn with it again)
const shown = computed(() => !store.hidden.get(path)?.has(selected.value));

const { data: properties } = useQuery(() =>
  propertiesQuery({ id: workspaceId, request: request.value, part: part.value ? selected.value : -1 }),
);
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <DetailHeader kind="GUI" :origin="located?.root.origin ?? ''" :name="fileName(path)" />
    <template v-if="part">
      <UBreadcrumb
        :items="crumbs"
        class="border-b border-default px-3.5 py-2"
        :ui="{ list: 'flex-wrap gap-x-1 gap-y-0.5', separatorIcon: 'size-3' }"
      >
        <template #item="{ item, active }">
          <button
            type="button"
            class="font-mono text-11.5 break-all text-start"
            :class="active ? 'text-highlighted' : 'text-toned hover:text-highlighted'"
            @click="selected = item.part"
          >
            {{ item.label }}
          </button>
        </template>
      </UBreadcrumb>
      <div class="flex h-8 shrink-0 items-center gap-2 border-b border-default bg-elevated px-3.5 text-12">
        <span class="text-muted">{{ part.kind }}</span>
        <span class="grow" />
        <USwitch
          :model-value="shown"
          size="xs"
          :label="shown ? 'Shown' : 'Hidden'"
          :ui="{ label: 'text-11.5 text-muted' }"
          @update:model-value="store.toggleHidden(path, selected)"
        />
      </div>
      <div class="min-h-0 flex-1 divide-y divide-default overflow-auto">
        <div
          v-for="property in properties"
          :key="property.key"
          class="flex items-baseline gap-3 px-3.5 py-1.5 text-11.5 odd:bg-elevated/40"
        >
          <span class="shrink-0 text-muted">{{ property.key }}</span>
          <span v-if="property.writtenBy" class="truncate text-11 text-dimmed">from {{ property.writtenBy }}</span>
          <span class="grow" />
          <ScriptSnippet :text="property.value" :name="path" inline class="min-w-0 text-end break-all" />
        </div>
      </div>
    </template>
    <p v-else class="px-3.5 py-2 text-11 text-dimmed">Click a widget in the picture, or put the caret in one.</p>
  </aside>
</template>
