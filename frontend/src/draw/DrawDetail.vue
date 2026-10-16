<script setup lang="ts">
/**
 * The panel of a drawn file, named by its kind's row (Widgets, Layers): under the file's header, the way down to the
 * selected widget as chips, or a coat's layers as a tree with an eye each (the one the pointer is on is the picture's
 * hovered element, outlined there); the element, a link to its code, whether it is shown for this session and, for a
 * coat's layer, what adds, removes or reorders one; the condition a widget is shown on, with the state each group it
 * reads is drawn at; and its properties as written, grouped by who wrote them, each a link to where it is written, or
 * for a coat one edited in place. The selection is the picture's or the caret's.
 */
import { computed, onScopeDispose, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import type { BreadcrumbItem, TreeItem } from "@nuxt/ui";
import type { Element as DrawElement, Property, Texture } from "@services/internal/draw";
import type { Location } from "@services/internal/lsp";
import { pathKey, useIdeRoots } from "../composables/useIdeRoots";
import { textAt } from "../editor/models";
import { viewsOf } from "../editor/tabs";
import { fileName } from "../lib/format";
import { propertiesQuery } from "../queries";
import { coatEntry, useDraw, useDrawStore } from "../stores/draw";
import DetailHeader from "../ui/DetailHeader.vue";
import ScriptSnippet from "../ui/ScriptSnippet.vue";
import CoatField, { COAT_FIELDS } from "./CoatField.vue";
import { choicesOf, coatNumber, coatOf, groupsOf, isEditable, stateOf, typeIcon, withDescendants } from "./widgets";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A widget on the way to the selected one: its key as the label, a name of its own ("" for none), its element. */
type Crumb = BreadcrumbItem & { name: string; element: number };

/** A coat's layer in the tree: its element. */
type Layer = TreeItem & { element: number };

/** What a coat's layer adds: an instance at the field's middle at half size, or a colored emblem with one, a line each
 * where its siblings are written over lines. */
const TEMPLATES = {
  instance: ["instance = {", "position = { 0.5 0.5 }", "scale = { 0.5 0.5 }", "}"],
  emblem: [
    "colored_emblem = {",
    'texture = ""',
    'color1 = "white"',
    "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }",
    "}",
  ],
};

const store = useDrawStore();
const { locate } = useIdeRoots(() => workspaceId);
const root = computed(() => locate(path)?.root);
const { file, request, list, elements } = useDraw(
  () => workspaceId,
  () => path,
);
const row = computed(() => viewsOf(path));
const isLayers = computed(() => row.value?.panel === "Layers");
/** Whether a coat edit may write at a location: the coat's own entry in this file, never an inherited layer, an alias's
 * target or a game's file. */
const canEdit = (location: Location) => isLayers.value && isEditable(path, file.value.entry, elements.value, location);
const isCoatEditable = computed(() => !!elements.value[0] && canEdit(elements.value[0].location));

// the selected element and the way down to it, root first
const selection = computed(() => file.value.selection);
const element = computed(() => elements.value[selection.value]);
const crumbs = computed(() =>
  [...(element.value?.ancestors ?? []), selection.value].flatMap((step): Crumb[] => {
    const crumb = elements.value[step];
    if (!crumb) return [];
    return [
      { label: crumb.key, name: crumb.name !== crumb.key ? crumb.name : "", icon: typeIcon(crumb.type), element: step },
    ];
  }),
);

/** A coat's layers under an element (-1 the coat itself). */
function layersUnder(parent: number): Layer[] {
  return elements.value.flatMap((one, index): Layer[] => {
    if (one.parent !== parent) return [];
    const children = layersUnder(index);
    return [
      { label: one.label, icon: typeIcon(one.type), element: index, children: children.length ? children : undefined },
    ];
  });
}
const layers = computed(() => layersUnder(-1));
// every layer open whenever a coat is drawn, each closed only until the next
const expanded = shallowRef<string[]>([]);
watch(list, () => (expanded.value = elements.value.map((_, index) => String(index))), { immediate: true });
const selectedLayer = computed<Layer | undefined>({
  get: () => {
    const open = [...layers.value];
    for (let layer = open.shift(); layer; layer = open.shift()) {
      if (layer.element === selection.value) return layer;
      open.push(...((layer.children ?? []) as Layer[]));
    }
    return undefined;
  },
  set: (layer) => {
    if (layer) file.value.selection = layer.element;
  },
});
// the elements hidden, by themselves or under one hidden
const hiddenAll = computed(() => withDescendants(elements.value, file.value.hidden));

/** Makes a step of the path the picture's hovered widget; -1 for none. */
const point = (step: number) => (file.value.hover = step);

/** A chip's look: the selected widget's in the accent, the hovered one's in the link color, any other plain. */
function chipClass(index: number): string {
  if (index === selection.value) return "border-primary/45 bg-(--accent-soft) text-primary";
  if (index === file.value.hover) return "border-secondary/45 bg-(--link-soft) text-secondary";
  return "border-transparent text-dimmed hover:bg-accented";
}

// a panel closed while a chip is pointed at leaves no mark on the picture
onScopeDispose(() => point(-1));

// whether the selected element itself is hidden for the session (one under a hidden element is drawn with it again)
const isShown = computed(() => !file.value.hidden.has(selection.value));

// the groups the selected element's condition reads, each with the value it is drawn at and its choices
const conditionGroups = computed(() => {
  const condition = element.value?.condition;
  if (!condition) return [];
  return groupsOf(condition).flatMap((index) => {
    const group = list.value?.groups?.[index];
    return group ? [{ call: group.call, value: stateOf(file.value, group), items: choicesOf(group) }] : [];
  });
});

const { data: properties } = useQuery(() =>
  propertiesQuery({ id: workspaceId, request: request.value, element: element.value ? selection.value : -1 }),
);

// the properties by who wrote them, the widget's own first, then each writer as it first appears (the sort is
// stable); a writer's location is where it writes its first row
const writers = computed(() =>
  [...Map.groupBy(properties.value ?? [], (property) => property.writer)]
    .sort(([a], [b]) => Number(a !== "") - Number(b !== ""))
    .map(([writer, rows]) => ({ writer, rows, location: rows[0].location })),
);

/** A location as a card's link writes it: the line alone in this file, else the file and the line. */
function linkText(location: Location): string {
  const line = location.range.start.line + 1;
  return pathKey(location.path) === pathKey(path) ? `L${line}` : `${location.rel}:${line}`;
}

/** Whether a property is edited in place: of a key with a row, its value the coat's own. */
const isField = (property: Property) => !!COAT_FIELDS[property.key.toLowerCase()] && canEdit(property.valueLocation);

/** The key a property's texture folder is written by: a pattern's, else the emblem's own. */
const textureKeyOf = (property: Property) =>
  property.key.toLowerCase() === "pattern" ? "pattern" : (element.value?.key ?? "");

/** The texture a property's tiles are colored as: the coat's pattern for a pattern, else the emblem's first
 * instance's. */
function textureOf(property: Property): Texture | undefined {
  const type = property.key.toLowerCase() === "pattern" ? "pattern" : "instance";
  const owner =
    element.value?.type === type
      ? element.value
      : elements.value.find((one) => one.parent === selection.value && one.type === type);
  return list.value?.textures?.[owner?.textures?.[0] ?? -1];
}

/** Where the last of a type is written under a holder and the elements its block also draws (an emblem's runs), among
 * those the coat writes itself; undefined for none. */
function lastOf(holder: number, type: string): Location | undefined {
  const start = elements.value[holder]?.location.range.start;
  const isRun = (one: DrawElement) =>
    one.location.range.start.line === start?.line && one.location.range.start.character === start.character;
  const holders = new Set(elements.value.flatMap((one, i) => (i === holder || isRun(one) ? [i] : [])));
  return elements.value
    .filter((one) => holders.has(one.parent) && one.type === type && one.key !== elements.value[one.parent]?.key)
    .map((one) => one.location)
    .filter(canEdit)
    .sort((a, b) => a.range.end.line - b.range.end.line || a.range.end.character - b.range.end.character)
    .at(-1);
}

/** What a layer adds under the selected one: an instance under an emblem, else an emblem. */
const added = computed(() => (element.value?.type === "emblem" ? "instance" : "emblem"));

/** Adds an instance to the selected emblem, or an emblem to the selected coat, after the last written of its kind,
 * else inside its block. */
function addLayer(): void {
  const holder = element.value;
  if (!holder) return;
  const lines = TEMPLATES[added.value];
  const last = lastOf(selection.value, added.value);
  if (last) void store.addAfter(path, last, lines);
  else void store.addInside(path, holder.location, lines.join(" "));
}

/** Moves the selected instance one place forward (1) or back (-1) among its coat's instances, by writing its depth
 * halfway to the instance past its neighbor, or a step past the neighbor where none is or the two are tied (equal
 * depths keep the written order); the list says which way a larger depth draws. */
function reorder(way: 1 | -1): void {
  const index = selection.value;
  const coat = coatOf(elements.value, index);
  const order = elements.value.flatMap((one, i) =>
    one.type === "instance" && coatOf(elements.value, i) === coat ? [i] : [],
  );
  const at = order.indexOf(index);
  const depthOf = (i: number | undefined) => Number(elements.value[i ?? -1]?.properties?.depth ?? 0);
  const [neighbor, past] = [order[at + way], order[at + 2 * way]];
  if (neighbor === undefined || !element.value) return;
  const toFront = list.value?.depthInFront ? 1 : -1;
  const isTied = past === undefined || depthOf(past) === depthOf(neighbor);
  const depth = isTied ? depthOf(neighbor) + way * toFront : (depthOf(neighbor) + depthOf(past)) / 2;
  const property = properties.value?.find((one) => one.key.toLowerCase() === "depth");
  const text = coatNumber(depth);
  if (property) void store.rewrite(path, property.valueLocation, text);
  else void store.addInside(path, element.value.location, `depth = ${text}`);
}

/** Copies the coat as the game's designer pastes it: its entry as written. */
function copyForGame(): void {
  const coat = elements.value[0];
  const text = coat && textAt(coat.location.path, coat.location.range);
  if (text) void Clipboard.SetText(text);
}

/** Replaces the coat with the game designer's copy, as one change it can undo; what nothing draws is listed below. */
async function pasteFromGame(): Promise<void> {
  const coat = elements.value[0];
  const text = await Clipboard.Text();
  if (coat && text.trim() && isCoatEditable.value)
    void store.rewrite(path, coat.location, coatEntry(file.value.entry, text));
}

/** Opens the new coat dialog on a copy of this coat, to write into a mod. */
function editCopy(): void {
  const coat = elements.value[0];
  const text = coat && textAt(coat.location.path, coat.location.range);
  if (text) store.coatDialog = { mode: "copy", id: file.value.entry, text, mod: "" };
}
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <DetailHeader :kind="row?.label ?? ''" :origin="root?.origin ?? ''" :name="fileName(path)">
      <template v-if="isLayers && list" #actions>
        <UTooltip text="Copy this coat's script for the game's designer">
          <UButton
            icon="i-lucide-copy"
            aria-label="Copy for the game"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            @click="copyForGame"
          />
        </UTooltip>
        <UTooltip v-if="isCoatEditable" text="Replace this coat with the game designer's copy">
          <UButton
            icon="i-lucide-clipboard-paste"
            aria-label="Paste from the game"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            @click="pasteFromGame"
          />
        </UTooltip>
        <UButton v-else label="Edit a copy" icon="i-lucide-copy-plus" size="xs" @click="editCopy" />
      </template>
    </DetailHeader>
    <!-- a coat's layers, the selected one marked; the row the pointer is on outlines its element -->
    <div
      v-if="isLayers && list"
      class="mx-3 mt-3 max-h-72 shrink-0 overflow-auto rounded-lg border border-default bg-default p-1"
      @mouseleave="point(-1)"
    >
      <UTree
        v-model="selectedLayer"
        v-model:expanded="expanded"
        :items="layers"
        :get-key="(layer) => String((layer as Layer).element)"
        color="neutral"
        size="sm"
      >
        <template #item-label="{ item }">
          <span
            class="truncate font-mono text-11.5"
            :class="hiddenAll.has((item as Layer).element) && 'opacity-50'"
            @mouseenter="point((item as Layer).element)"
            >{{ item.label }}</span
          >
        </template>
        <template #item-trailing="{ item }">
          <UButton
            :icon="hiddenAll.has((item as Layer).element) ? 'i-lucide-eye-off' : 'i-lucide-eye'"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            :aria-label="hiddenAll.has((item as Layer).element) ? 'Show' : 'Hide'"
            @click.stop="store.toggleHidden(path, [(item as Layer).element])"
          />
        </template>
      </UTree>
    </div>
    <template v-if="element && list">
      <!-- the way down to the selected widget: the chip the pointer is on outlines its widget -->
      <div
        v-if="!isLayers"
        class="mx-3 mt-3 rounded-lg border border-default bg-default p-1.25"
        @mouseleave="point(-1)"
      >
        <UBreadcrumb
          :items="crumbs"
          color="neutral"
          :ui="{ list: 'flex-wrap gap-x-px gap-y-1', separatorIcon: 'size-3 text-dimmed' }"
        >
          <template #item="{ item }">
            <button
              type="button"
              class="flex h-6 max-w-full min-w-0 items-center gap-1.25 rounded-sm border ps-1.25 pe-1.75 font-mono text-11.5 font-normal whitespace-nowrap"
              :class="chipClass(item.element)"
              :title="item.name ? `${item.label} ${item.name}` : item.label"
              @mouseenter="point(item.element)"
              @focus="point(item.element)"
              @blur="point(-1)"
              @click="file.selection = item.element"
            >
              <UIcon :name="item.icon ?? ''" class="size-3.25 shrink-0" />
              <span class="truncate text-highlighted">{{ item.label }}</span>
              <span v-if="item.name" class="truncate text-dimmed">{{ item.name }}</span>
            </button>
          </template>
        </UBreadcrumb>
      </div>
      <div class="mt-3 flex h-10 shrink-0 items-center gap-2 border-y border-default px-3">
        <span class="min-w-0 truncate font-mono text-13 font-semibold text-highlighted">{{ element.label }}</span>
        <span class="shrink-0 text-11.5 text-dimmed">{{ element.type }}</span>
        <span class="grow" />
        <!-- a coat's layer: what adds one under it, moves it among its coat's instances, or removes it -->
        <template v-if="canEdit(element.location)">
          <UTooltip v-if="element.type === 'emblem' || selection === 0" :text="`Add an ${added}`">
            <UButton
              icon="i-lucide-plus"
              :aria-label="`Add an ${added}`"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              @click="addLayer"
            />
          </UTooltip>
          <template v-if="element.type === 'instance' && element.key === 'instance'">
            <UTooltip text="Draw it in front of the next">
              <UButton
                icon="i-lucide-arrow-up"
                aria-label="Forward"
                variant="ghost"
                color="neutral"
                size="xs"
                square
                @click="reorder(1)"
              />
            </UTooltip>
            <UTooltip text="Draw it behind the one before">
              <UButton
                icon="i-lucide-arrow-down"
                aria-label="Back"
                variant="ghost"
                color="neutral"
                size="xs"
                square
                @click="reorder(-1)"
              />
            </UTooltip>
          </template>
          <UTooltip v-if="selection > 0" text="Remove it from the file">
            <UButton
              icon="i-lucide-trash-2"
              aria-label="Remove"
              variant="ghost"
              color="error"
              size="xs"
              square
              @click="store.removeBlock(path, element.location)"
            />
          </UTooltip>
        </template>
        <UTooltip text="Put the caret where this is written">
          <UButton
            label="Go to code"
            trailing-icon="i-lucide-arrow-up-right"
            variant="ghost"
            color="neutral"
            size="xs"
            class="shrink-0"
            @click="store.goTo(path, element.location)"
          />
        </UTooltip>
        <USwitch
          :model-value="isShown"
          size="xs"
          :label="isShown ? 'Shown' : 'Hidden'"
          :ui="{ label: 'text-12 text-muted' }"
          class="shrink-0"
          @update:model-value="store.toggleHidden(path, [selection])"
        />
      </div>
      <!-- the condition the widget is shown on: the game answers it; the window is drawn at one value of each group -->
      <UAlert
        v-if="conditionGroups.length"
        title="Shown when"
        :description="element.visibleBinding"
        color="secondary"
        variant="subtle"
        orientation="vertical"
        class="mx-3 mt-2.5 w-auto shrink-0 px-2.5 py-2"
        :ui="{
          title: 'text-11.5 font-normal text-toned',
          description: 'mt-1 font-mono text-11.5 break-all text-(--syn-ref) opacity-100',
          actions: 'mt-1.5 w-full flex-col items-stretch gap-1',
        }"
      >
        <template #actions>
          <div v-for="group in conditionGroups" :key="group.call" class="flex items-center gap-2">
            <span class="min-w-0 grow truncate font-mono text-11 text-dimmed" :title="group.call">{{
              group.call
            }}</span>
            <USelect
              :model-value="group.value"
              :items="group.items"
              size="xs"
              class="w-40 shrink-0"
              @update:model-value="(value) => store.setState(path, group.call, String(value))"
            />
          </div>
        </template>
      </UAlert>
      <!-- one card per writer: its header names it and links to where it writes, each row a link of its own or, for a
           coat's property, edited in place -->
      <div class="flex min-h-0 flex-1 flex-col gap-2.5 overflow-auto p-3">
        <section
          v-for="group in writers"
          :key="group.writer"
          class="shrink-0 overflow-hidden rounded-lg border border-default bg-default"
        >
          <div
            class="flex h-7.5 items-center gap-1.5 border-b border-default bg-elevated ps-2.5 pe-1.5 text-11.5 whitespace-nowrap text-dimmed"
          >
            <UIcon :name="group.writer ? 'i-lucide-layers' : 'i-lucide-pen'" class="size-3.25 shrink-0" />
            <template v-if="group.writer">
              <span>from</span>
              <span class="min-w-0 truncate font-mono font-medium text-highlighted" :title="group.writer">
                {{ group.writer }}
              </span>
            </template>
            <span v-else>{{ isLayers ? "This layer" : "This widget" }}</span>
            <UTooltip text="Open where it is written">
              <UButton
                :label="linkText(group.location)"
                trailing-icon="i-lucide-arrow-up-right"
                variant="ghost"
                color="neutral"
                size="xs"
                class="ms-auto min-w-0 px-1.25 py-0.5 font-mono text-11 text-dimmed hover:bg-accented hover:text-secondary"
                :ui="{ label: 'truncate', trailingIcon: 'size-2.75' }"
                @click="store.goTo(path, group.location)"
              />
            </UTooltip>
          </div>
          <template v-for="property in group.rows" :key="property.key">
            <div
              v-if="isField(property)"
              class="grid min-h-8 w-full grid-cols-[112px_minmax(0,1fr)] items-center gap-x-3 ps-2.5 pe-2 text-11.5 even:bg-accented/30"
            >
              <span class="truncate font-medium text-toned">{{ property.key }}</span>
              <CoatField
                :workspace-id="workspaceId"
                :path="path"
                :property="property"
                :evaluated="element.properties?.[property.key.toLowerCase()] ?? ''"
                :texture-key="textureKeyOf(property)"
                :texture="textureOf(property)"
              />
            </div>
            <button
              v-else
              type="button"
              class="group grid h-7 w-full grid-cols-[112px_minmax(0,1fr)_14px] items-center gap-x-3 ps-2.5 pe-2 text-start text-11.5 even:bg-accented/30 hover:bg-accented"
              :title="`${property.key} = ${property.value}`"
              @click="store.goTo(path, property.location)"
            >
              <span class="truncate font-medium text-toned">{{ property.key }}</span>
              <span class="min-w-0 truncate text-end"
                ><ScriptSnippet :text="property.value" :name="path" inline
              /></span>
              <UIcon
                name="i-lucide-arrow-up-right"
                class="size-3.25 text-secondary opacity-0 group-hover:opacity-100"
              />
            </button>
          </template>
        </section>
      </div>
    </template>
    <UEmpty
      v-else-if="!isLayers"
      icon="i-lucide-mouse-pointer-click"
      title="Select a widget"
      description="Click it in the picture, or put the caret in its code."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </aside>
</template>
