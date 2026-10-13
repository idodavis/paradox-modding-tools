<script setup lang="ts">
/**
 * The Widgets panel of a GUI file: under the file's header, the sections of one accordion: the window's widgets as a
 * tree, the selected widget's properties (an input each, which edits its statement through the editor), the sample
 * data its bindings read, and its script.
 */
import { computed, nextTick, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import type { TreeItem } from "@nuxt/ui";
import { Edit, SetFact } from "@services/drawservice";
import type { Change, Fact, Part, Property } from "@services/internal/draw";
import { useDraw } from "../composables/useDraw";
import { useIdeRoots } from "../composables/useIdeRoots";
import { applyFix } from "../editor/models";
import { fileName } from "../lib/format";
import { linesQuery, propertiesQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";
import DetailHeader from "../ui/DetailHeader.vue";
import ScriptSnippet from "../ui/ScriptSnippet.vue";
import SourceLink from "../ui/SourceLink.vue";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A part as a row of the tree: its index, and what its row says of it. */
type WidgetItem = TreeItem & { part: number; kind: string; visibility: string };

const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);
const located = computed(() => locate(path));
const { selected, request, list, parts } = useDraw(
  () => workspaceId,
  () => path,
);

const SECTIONS = [
  { label: "Widgets", value: "widgets", slot: "widgets" as const },
  { label: "Properties", value: "properties", slot: "properties" as const },
  { label: "Sample data", value: "samples", slot: "samples" as const },
  { label: "Script", value: "script", slot: "script" as const },
];
const OPEN = SECTIONS.map((section) => section.value);
// the kinds of answer a fact takes: an object is a scope, which has no value to give
const FACT_KINDS = ["bool", "number", "text", "list"];

// the widgets: the parts, in the order Go lists them, nested under their parent
const tree = computed(() => {
  const nodes = parts.value.map((part, index): WidgetItem => ({
    part: index,
    label: part.name || part.key,
    kind: part.kind,
    visibility: part.visibility,
    defaultExpanded: true,
  }));
  const roots: WidgetItem[] = [];
  parts.value.forEach((part, index) => {
    const parent = nodes[part.parent];
    if (parent) (parent.children ??= []).push(nodes[index]);
    else roots.push(nodes[index]);
  });
  return { roots, nodes };
});
/** The key of a row of the tree: its part's number. */
const partKey = (item: WidgetItem) => String(item.part);

// the row of the selected part comes into view where the pane or the code chose it
const widgets = useTemplateRef("widgets");
watch(selected, async () => {
  await nextTick();
  widgets.value?.querySelector("[data-selected]")?.scrollIntoView({ block: "nearest" });
});

// the selected part's properties, read-only where the widget is written in the game's files
const part = computed(() => parts.value[selected.value]);
const readOnly = computed(() => part.value?.location.origin === store.originVanilla);
const { data: properties } = useQuery(() =>
  propertiesQuery({ id: workspaceId, request: request.value, part: part.value ? selected.value : -1 }),
);

// the neighbors a widget moves past: the parts of its parent, in the file it is written in
const neighbors = computed(() => {
  const own = part.value;
  const sibling = (one: Part) => one.parent === own?.parent && one.location.path === own.location.path;
  return {
    previous: own ? parts.value.findLastIndex((one, index) => index < selected.value && sibling(one)) : -1,
    next: own ? parts.value.findIndex((one, index) => index > selected.value && sibling(one)) : -1,
  };
});

// an edit is Go's text edits for the change, applied through the editor to the file the part is written in
const { mutate: edit } = useMutation({
  mutation: async (change: Change) => {
    const place = parts.value[change.part]?.location;
    const edits = await Edit(workspaceId, request.value, change);
    if (place && edits?.length) await applyFix({ title: "Edit widget", edit: { changes: { [place.path]: edits } } });
  },
  // the parts after a removed or moved one change their numbers
  onSuccess: (_, change) => {
    if (change.remove || change.before >= 0) selected.value = -1;
  },
});

/** Writes a property's value through an edit, when it differs. */
function setProperty(property: Property, value: string): void {
  if (value !== property.value) edit({ part: selected.value, key: property.key, value, remove: false, before: -1 });
}

/** A texture, font or choice's options, with the value written when the dumps do not list it. */
function optionsOf(property: Property): string[] {
  const options = property.options ?? [];
  return options.includes(property.value) ? options : [property.value, ...options];
}

// the sample data: the answers the bindings ask, kept in the workspace; an emptied answer is taken back
const facts = computed(() => (list.value?.facts ?? []).filter((fact) => FACT_KINDS.includes(fact.returns)));
const { mutate: answer } = useMutation({ mutation: (fact: Fact) => SetFact(workspaceId, fact) });

/** Answers a fact with a value, or takes the answer back with an empty one. */
function setFact(fact: Fact, value: string): void {
  answer({ ...fact, value, known: value !== "" });
}

// the selected part's statement: its first lines, and a mark where it runs on
const place = computed(() => part.value?.location);
const { data: snippets } = useQuery(() =>
  linesQuery({
    id: workspaceId,
    places: place.value
      ? [
          {
            path: place.value.path,
            line: place.value.range.start.line,
            col: place.value.range.start.character,
            definition: true,
          },
        ]
      : [],
  }),
);
const rows = computed(() => snippets.value?.[0] ?? []);
const truncated = computed(() => rows.value.at(-1) === "…");
const script = computed(() => (truncated.value ? rows.value.slice(0, -1) : rows.value).join("\n"));
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <DetailHeader kind="GUI" :origin="located?.root.origin ?? ''" :name="fileName(path)" />
    <UAccordion type="multiple" :items="SECTIONS" :default-value="OPEN" class="min-h-0 flex-1 overflow-auto">
      <template #widgets-body>
        <div ref="widgets">
          <UTree
            v-if="tree.roots.length"
            :items="tree.roots"
            :model-value="tree.nodes[selected]"
            :get-key="partKey"
            @update:model-value="(item) => (selected = item.part)"
          >
            <template #item-label="{ item }">
              <span :class="{ 'opacity-50': item.visibility === 'hidden' }">{{ item.label }}</span>
              <span class="ms-1.5 font-sans text-11 text-dimmed">{{ item.kind }}</span>
              <span v-if="item.visibility === 'unknown'" class="ms-1 text-warning" title="Depends on a fact not known"
                >?</span
              >
            </template>
          </UTree>
          <p v-else class="px-3 py-1.5 text-11 text-dimmed">No render yet.</p>
        </div>
      </template>
      <template #properties-body>
        <template v-if="part">
          <div class="flex items-center gap-1 px-3.5 py-1.5">
            <span class="min-w-0 grow truncate font-mono text-12 text-toned">{{ part.name || part.key }}</span>
            <UButton
              icon="i-lucide-arrow-up"
              variant="ghost"
              size="xs"
              square
              aria-label="Move up"
              :disabled="readOnly || neighbors.previous < 0"
              @click="edit({ part: selected, key: '', value: '', remove: false, before: neighbors.previous })"
            />
            <UButton
              icon="i-lucide-arrow-down"
              variant="ghost"
              size="xs"
              square
              aria-label="Move down"
              :disabled="readOnly || neighbors.next < 0"
              @click="edit({ part: neighbors.next, key: '', value: '', remove: false, before: selected })"
            />
            <UButton
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              size="xs"
              square
              aria-label="Remove widget"
              :disabled="readOnly"
              @click="edit({ part: selected, key: '', value: '', remove: true, before: -1 })"
            />
          </div>
          <p v-if="readOnly" class="px-3.5 pb-1.5 text-11 text-dimmed">
            This widget is written in the game's files, which are read only.
          </p>
          <UFormField
            v-for="property in properties"
            :key="property.key"
            :label="property.key"
            :description="property.writtenBy || undefined"
            orientation="horizontal"
          >
            <USwitch
              v-if="property.kind === 'bool'"
              :model-value="property.value === 'yes'"
              :disabled="readOnly"
              @update:model-value="setProperty(property, $event ? 'yes' : 'no')"
            />
            <USelectMenu
              v-else-if="property.options?.length"
              :model-value="property.value"
              :items="optionsOf(property)"
              :disabled="readOnly"
              virtualize
              size="sm"
              class="w-40"
              @update:model-value="setProperty(property, $event)"
            />
            <UInput
              v-else
              :model-value="property.value"
              :model-modifiers="{ lazy: true }"
              :disabled="readOnly"
              size="sm"
              class="w-40"
              @update:model-value="setProperty(property, $event)"
            />
          </UFormField>
        </template>
        <p v-else class="px-3 py-1.5 text-11 text-dimmed">Select a widget.</p>
      </template>
      <template #samples-body>
        <UFormField
          v-for="fact in facts"
          :key="fact.call"
          :label="fact.call"
          :description="fact.function ? `${fact.function} · ${fact.returns}` : fact.returns"
          :ui="{ label: 'font-mono' }"
          orientation="horizontal"
        >
          <div class="flex items-center gap-1">
            <USwitch
              v-if="fact.returns === 'bool'"
              :model-value="fact.known && fact.value === 'yes'"
              @update:model-value="setFact(fact, $event ? 'yes' : 'no')"
            />
            <UInput
              v-else
              :model-value="fact.value"
              :model-modifiers="{ lazy: true }"
              :type="fact.returns === 'text' ? 'text' : 'number'"
              placeholder="not known"
              size="sm"
              class="w-32"
              @update:model-value="setFact(fact, String($event ?? ''))"
            />
            <UButton
              icon="i-lucide-rotate-ccw"
              variant="ghost"
              size="xs"
              square
              aria-label="Take the answer back"
              :class="{ invisible: !fact.known }"
              @click="setFact(fact, '')"
            />
          </div>
        </UFormField>
        <p v-if="!facts.length" class="px-3 py-1.5 text-11 text-dimmed">No bindings to set.</p>
      </template>
      <template #script-body>
        <template v-if="place && script">
          <SourceLink :place="place" :line="place.range.start.line" class="mx-3.5 mt-1" />
          <ScriptSnippet :text="script" :name="place.rel" :from-line="place.range.start.line" class="py-1.5" />
          <p v-if="truncated" class="ps-12 pb-1.5 font-mono text-12 text-dimmed">…</p>
        </template>
        <p v-else class="px-3 py-1.5 text-11 text-dimmed">Select a widget.</p>
      </template>
    </UAccordion>
  </aside>
</template>
