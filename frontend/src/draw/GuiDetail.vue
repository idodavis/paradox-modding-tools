<script setup lang="ts">
/**
 * The Widgets panel of a GUI file: under the file's header, where the selected widget sits (its ancestors, each a
 * click away), whether it is shown for this session, and its properties as written in the code's colors, grouped by
 * who wrote them: the widget itself first, then each type or template it comes from. The selection is the pane's or
 * the caret's.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import type { BreadcrumbItem } from "@nuxt/ui";
import type { Property } from "@services/internal/draw";
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

// the properties by who wrote them, the widget's own first, then each writer as it first appears
const groups = computed(() => {
  const by = new Map<string, Property[]>([["", []]]);
  for (const property of properties.value ?? []) {
    const rows = by.get(property.writtenBy) ?? [];
    rows.push(property);
    by.set(property.writtenBy, rows);
  }
  return [...by].filter(([, rows]) => rows.length).map(([writer, rows]) => ({ writer, rows }));
});
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
      <!-- one card per writer: a filled header naming it, its rows inside -->
      <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3.5">
        <section
          v-for="group in groups"
          :key="group.writer"
          class="shrink-0 overflow-hidden rounded-lg border border-default bg-elevated"
        >
          <div class="flex h-7 items-center gap-1.5 border-b border-default bg-accented/40 px-3 text-11 text-toned">
            <UIcon :name="group.writer ? 'i-lucide-layers-2' : 'i-lucide-square-dashed'" class="size-3.5 text-dimmed" />
            <template v-if="group.writer">
              <span class="text-dimmed">from</span>
              <span class="truncate font-mono font-semibold" :title="group.writer">{{ group.writer }}</span>
            </template>
            <span v-else class="font-semibold">This widget</span>
          </div>
          <div class="divide-y divide-default">
            <div
              v-for="property in group.rows"
              :key="property.key"
              class="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] items-baseline gap-x-4 px-3 py-1.5 text-11.5 even:bg-accented/20"
            >
              <span class="truncate font-medium text-toned" :title="property.key">{{ property.key }}</span>
              <span class="min-w-0 truncate text-end" :title="property.value">
                <ScriptSnippet :text="property.value" :name="path" inline />
              </span>
            </div>
          </div>
        </section>
      </div>
    </template>
    <p v-else class="px-3.5 py-2 text-11 text-dimmed">Click a widget in the picture, or put the caret in one.</p>
  </aside>
</template>
