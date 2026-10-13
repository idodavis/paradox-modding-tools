<script setup lang="ts">
/**
 * The Widgets panel of a GUI file: under the file's header, where the selected widget sits (its ancestors, each a
 * click away) and its properties as written, each with who wrote it: the widget, or the type or template it comes
 * from. The selection is the pane's or the caret's.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import type { BreadcrumbItem } from "@nuxt/ui";
import { useDraw } from "../composables/useDraw";
import { useIdeRoots } from "../composables/useIdeRoots";
import { fileName } from "../lib/format";
import { propertiesQuery } from "../queries";
import DetailHeader from "../ui/DetailHeader.vue";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A widget on the way to the selected one: its row of the breadcrumb and its part. */
type Crumb = BreadcrumbItem & { part: number };

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

const { data: properties } = useQuery(() =>
  propertiesQuery({ id: workspaceId, request: request.value, part: part.value ? selected.value : -1 }),
);
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <DetailHeader kind="GUI" :origin="located?.root.origin ?? ''" :name="fileName(path)" />
    <template v-if="part">
      <UBreadcrumb :items="crumbs" class="border-b border-default px-3.5 py-2">
        <template #item="{ item, active }">
          <button
            type="button"
            class="font-mono text-11.5"
            :class="active ? 'text-highlighted' : 'text-toned hover:text-highlighted'"
            @click="selected = item.part"
          >
            {{ item.label }}
          </button>
        </template>
      </UBreadcrumb>
      <div class="min-h-0 flex-1 overflow-auto px-3.5 py-2">
        <div class="pb-1.5 text-11 text-dimmed">{{ part.kind }}</div>
        <div v-for="property in properties" :key="property.key" class="py-1 text-11.5">
          <div class="flex gap-3">
            <span class="shrink-0 font-mono text-toned">{{ property.key }}</span>
            <span class="min-w-0 grow text-end font-mono break-all text-highlighted">{{ property.value }}</span>
          </div>
          <div v-if="property.writtenBy" class="text-11 text-dimmed">from {{ property.writtenBy }}</div>
        </div>
      </div>
    </template>
    <p v-else class="px-3.5 py-2 text-11 text-dimmed">Click a widget in the picture, or put the caret in one.</p>
  </aside>
</template>
