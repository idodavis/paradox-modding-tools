<script setup lang="ts">
/**
 * The Layers panel of a coat of arms: a row for each statement the coat writes in the order written (its pattern, its
 * emblems and its subs, one row for the runs of an emblem depth splits), each with an eye, an emblem or a sub with a
 * grip that a drop moves in the file; and under it the selected row's card: its texture, its colors, an emblem's
 * mask, and its instances as a table, the selected one's cells its inputs. The pointer on a row outlines it in the
 * picture; a row, a table row or a layer picked in the picture select one another.
 */
import { computed, onScopeDispose, shallowRef, useTemplateRef } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate } from "@vueuse/core";
import { insertNodeAt, removeNode, useSortable } from "@vueuse/integrations/useSortable";
import type { SortableEvent } from "sortablejs";
import type { Property } from "@services/internal/draw";
import type { Location } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { plural } from "../lib/format";
import { colorsQuery, propertiesQuery } from "../queries";
import { isAtOrBefore, useDraw, useDrawStore } from "../stores/draw";
import CoatField from "./CoatField.vue";
import TexturePicker from "./TexturePicker.vue";
import { isEditable } from "./coat";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A row of the list: what one statement of the coat draws, its layers (an emblem's runs, a sub's instances). */
type Row = { kind: "pattern" | "emblem" | "sub"; layers: number[] };

/** A new instance, on one line as the games write most: at the field's middle at half size. */
const INSTANCE = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }";

/** The named color a new emblem writes as its first slot; its others are the coat's. */
const EMBLEM_COLOR = "white";

/** The lines a new colored emblem is written as, a line each as the games write every emblem: the texture picked,
 * its first color, and a new instance. */
const emblemLines = (texture: string) => [
  "colored_emblem = {",
  `texture = "${texture}"`,
  `color1 = "${EMBLEM_COLOR}"`,
  INSTANCE,
  "}",
];

/** The order the kinds are listed in. */
const KINDS = ["pattern", "emblem", "sub"] as const;

/** The color slots a card shows, the three its shaders read. */
const SLOTS = ["color1", "color2", "color3"];

const store = useDrawStore();
const { file, request, coat, elements, render } = useDraw(
  () => workspaceId,
  () => path,
);
const layers = computed(() => coat.value?.layers ?? []);
/** Whether a coat edit may write at a location: the coat's own entry in this file. */
const canEdit = (location: Location) => isEditable(path, file.value.entry, elements.value, location);

/** The order two statements are listed in: this file's first, then where each is written. */
const writtenOrder = (first: Location, second: Location) =>
  Number(pathKey(first.path) !== pathKey(path)) - Number(pathKey(second.path) !== pathKey(path)) ||
  (isAtOrBefore(first.range.start, second.range.start) ? -1 : 1);

// the coat's rows: its own layers, one per statement, by kind and then where each is written
const rows = computed(() => {
  const own = layers.value.flatMap((layer, index) => (layer.parent === 0 ? [index] : []));
  const statements = Map.groupBy(own, (index) => JSON.stringify(layers.value[index].location));
  return [...statements.values()]
    .map((run): Row => ({ kind: layers.value[run[0]].type as Row["kind"], layers: run }))
    .sort(
      (first, second) =>
        KINDS.indexOf(first.kind) - KINDS.indexOf(second.kind) || writtenOrder(locationOf(first), locationOf(second)),
    );
});
const patternRow = computed(() => rows.value.find((row) => row.kind === "pattern"));
const emblemRows = computed(() => rows.value.filter((row) => row.kind === "emblem"));
const subRows = computed(() => rows.value.filter((row) => row.kind === "sub"));

/** Where a row's statement is written. */
const locationOf = (row: Row) => layers.value[row.layers[0]].location;

/** A row's instances in the order written: an emblem's instance layers, a sub's own layers. */
const instancesOf = (row: Row) =>
  row.kind !== "emblem"
    ? row.layers
    : layers.value
        .flatMap((layer, index) => (row.layers.includes(layer.parent) ? [index] : []))
        .sort((first, second) => Number(layers.value[first].name) - Number(layers.value[second].name));

/** Whether the session hides every layer of a row. */
const isHidden = (row: Row) => row.layers.every((layer) => file.value.hidden.has(layer));

/** What a row says of itself: its kind, the file or the parent it draws, and its instances or that it is hidden. */
function rowText(row: Row): { kind: string; name: string; detail: string } {
  const first = layers.value[row.layers[0]];
  const kind = { pattern: "Pattern", emblem: first.key === "textured_emblem" ? "Textured" : "Emblem", sub: "Sub" };
  const detail = isHidden(row) ? "hidden" : row.kind === "pattern" ? "" : plural(instancesOf(row).length, "instance");
  return { kind: kind[row.kind], name: first.name, detail };
}

// where depth orders the coat (an instance of its own writes one other than 0): each instance's place from the back,
// in the paint order Go lists them
const ownInstances = computed(() =>
  layers.value.flatMap((layer, index) =>
    layer.type === "instance" && layers.value[layer.parent]?.parent === 0 ? [index] : [],
  ),
);
const isDepthOrdered = computed(() =>
  ownInstances.value.some((index) => Number(layers.value[index].properties?.depth ?? 0) !== 0),
);

// the selected row, the one whose layers hold the selection, and the selected instance, else its first
const selection = computed(() => file.value.selection);
const row = computed(() =>
  rows.value.find((one) =>
    one.layers.some((layer) => layer === selection.value || layers.value[selection.value]?.ancestors?.includes(layer)),
  ),
);
const instances = computed(() => (row.value ? instancesOf(row.value) : []));
const instance = computed(
  () =>
    instances.value.find(
      (one) => one === selection.value || !!layers.value[selection.value]?.ancestors?.includes(one),
    ) ??
    instances.value[0] ??
    -1,
);
// the layer the card writes: the coat for its pattern (whose colors are the coat's), else the row's first
const cardLayer = computed(() => (row.value?.kind === "pattern" ? 0 : (row.value?.layers[0] ?? -1)));
const { data: cardProperties } = useQuery(() =>
  propertiesQuery({ id: workspaceId, request: request.value, render: render.value, element: cardLayer.value }),
);
const { data: instanceProperties } = useQuery(() =>
  propertiesQuery({
    id: workspaceId,
    request: request.value,
    render: render.value,
    element: row.value ? instance.value : -1,
  }),
);
/** The property that writes a key, of the card's or the selected instance's. */
const propertyOf = (properties: Property[] | undefined, key: string) =>
  properties?.find((property) => property.key.toLowerCase() === key);
/** Whether a layer's value is shown only: a parent coat writes it, or the coat is not this file's to write. */
const isReadOnly = (layer: number, key: string, properties: Property[] | undefined) =>
  !canEdit(layers.value[layer].location) || !!propertyOf(properties, key)?.writer;

// a row of the list, written once for the pattern's, the emblems' and the subs'; and the writer of a value a parent
// coat writes, which the card shows beside it with a link to where it is written
const [DefineRow, ReuseRow] = createReusableTemplate<{ one: Row }>();
const [DefineWriter, ReuseWriter] = createReusableTemplate<{ keys: string[] }>();

/** The first of some keys' properties of the card a parent coat writes; undefined where the layer writes them. */
const inheritedOf = (keys: string[]) =>
  keys.map((key) => propertyOf(cardProperties.value, key)).find((property) => !!property?.writer);

/** Makes a layer the picture's hovered one; -1 for none. */
const point = (layer: number) => (file.value.hover = layer);
onScopeDispose(() => point(-1));

/** Adds an instance to an emblem after its last written, else inside its block. */
function addInstance(emblem: Row): void {
  const explicit = instancesOf(emblem).filter((index) => layers.value[index].key === "instance");
  const last = explicit
    .map((index) => layers.value[index].location)
    .filter(canEdit)
    .at(-1);
  if (last) void store.addAfter(path, last, [INSTANCE]);
  else void store.addInside(path, locationOf(emblem), [INSTANCE]);
}

// the texture picker a new emblem starts from, its tiles colored as the emblem will be: its first slot the named
// color it writes, its others the coat's
const isAdding = shallowRef(false);
const { data: colors } = useQuery(() => colorsQuery({ id: isAdding.value ? workspaceId : "" }));
const emblemColors = computed(() =>
  (layers.value[0]?.colors ?? []).map((css, slot) => (slot === 1 ? (colors.value?.[EMBLEM_COLOR] ?? css) : css)),
);

/** Adds an emblem painting a texture to the coat after its last, else inside the coat, and closes the picker. */
function addEmblem(texture: string): void {
  isAdding.value = false;
  const last = emblemRows.value.map(locationOf).filter(canEdit).at(-1);
  if (last) void store.addAfter(path, last, emblemLines(texture));
  else void store.addInside(path, layers.value[0].location, emblemLines(texture));
}

/** Moves a dragged row's statement before or after the row it was dropped on, in the file, as one change; the list
 * puts the row back, as the coat drawn again moves it. A row the coat does not write itself stays where it is. */
function drop(kindRows: Row[], event: SortableEvent): void {
  removeNode(event.item);
  insertNodeAt(event.from, event.item, event.oldIndex ?? 0);
  const [from, to] = [event.oldIndex ?? 0, event.newIndex ?? 0];
  const [moved, sibling] = [kindRows[from], kindRows[to]];
  if (from === to || !moved || !sibling || !canEdit(locationOf(moved)) || !canEdit(locationOf(sibling))) return;
  void store.moveBlock(path, locationOf(moved), locationOf(sibling), to > from);
}
const SORTING = { handle: ".pmt-grip", animation: 150, watchElement: true };
useSortable(useTemplateRef<HTMLElement>("emblemList"), emblemRows, {
  ...SORTING,
  onUpdate: (event: SortableEvent) => drop(emblemRows.value, event),
});
useSortable(useTemplateRef<HTMLElement>("subList"), subRows, {
  ...SORTING,
  onUpdate: (event: SortableEvent) => drop(subRows.value, event),
});
</script>

<template>
  <DefineRow v-slot="{ one }">
    <div
      class="flex h-7 min-w-0 cursor-pointer items-center gap-2 ps-2 pe-2.5 text-12 whitespace-nowrap text-toned"
      :class="[
        one === row && 'bg-(--accent-soft) text-highlighted shadow-[inset_2px_0_0_var(--accent)]',
        isHidden(one) && 'opacity-50',
      ]"
      @click="file.selection = one.layers[0]"
      @mouseenter="point(one.layers[0])"
    >
      <UIcon
        name="i-lucide-grip-vertical"
        class="pmt-grip size-3.5 shrink-0 text-dimmed"
        :class="one.kind === 'pattern' || !canEdit(locationOf(one)) ? 'invisible' : 'cursor-grab'"
      />
      <UButton
        :icon="isHidden(one) ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :aria-label="isHidden(one) ? 'Show' : 'Hide'"
        variant="link"
        color="neutral"
        size="xs"
        square
        class="p-0"
        @click.stop="store.toggleHidden(path, one.layers)"
      />
      <span class="text-dimmed">{{ rowText(one).kind }}</span>
      <span class="min-w-0 truncate font-mono text-11.5 text-highlighted" :title="rowText(one).name">{{
        rowText(one).name
      }}</span>
      <span v-if="one.kind === 'pattern'" class="ms-auto flex gap-0.75">
        <i
          v-for="slot in [1, 2, 3]"
          :key="slot"
          class="size-3 rounded-sm"
          :style="{ background: layers[0].colors?.[slot] || 'transparent' }"
        />
      </span>
      <span v-else class="ms-auto text-11 text-dimmed">{{ rowText(one).detail }}</span>
    </div>
  </DefineRow>
  <DefineWriter v-slot="{ keys }">
    <template v-for="(property, ordinal) in [inheritedOf(keys)]" :key="ordinal">
      <UButton
        v-if="property"
        :label="`from ${property.writer}`"
        variant="link"
        color="neutral"
        size="xs"
        class="shrink-0 p-0 text-11 text-dimmed"
        @click="store.goTo(path, property.location)"
      />
    </template>
  </DefineWriter>
  <div v-if="coat" class="flex min-h-0 flex-1 flex-col">
    <!-- the rows: the pattern always behind, then the emblems and the subs, each in the order written -->
    <div class="flex h-9 shrink-0 items-center gap-1.5 border-b border-default ps-3.5 pe-1.5 text-12 font-semibold">
      Layers<span class="grow" />
      <UPopover v-if="canEdit(layers[0].location)" v-model:open="isAdding" :content="{ side: 'left', align: 'start' }">
        <UTooltip text="Add an emblem">
          <UButton icon="i-lucide-plus" aria-label="Add an emblem" variant="ghost" color="neutral" size="xs" square />
        </UTooltip>
        <template #content>
          <TexturePicker
            :workspace-id="workspaceId"
            texture-key="colored_emblem"
            :colors="emblemColors"
            @pick="addEmblem"
          />
        </template>
      </UPopover>
    </div>
    <div class="max-h-[40%] shrink-0 overflow-auto py-1" @mouseleave="point(-1)">
      <ReuseRow v-if="patternRow" :one="patternRow" />
      <div ref="emblemList">
        <ReuseRow v-for="one in emblemRows" :key="JSON.stringify(locationOf(one))" :one="one" />
      </div>
      <div ref="subList"><ReuseRow v-for="one in subRows" :key="JSON.stringify(locationOf(one))" :one="one" /></div>
      <p v-if="isDepthOrdered" class="px-3.5 pt-1.5 text-11 text-dimmed">
        Depth orders this coat: a larger depth is drawn behind. A drop changes the order among equal depths.
      </p>
    </div>
    <!-- the selected row's card -->
    <div v-if="row" class="flex min-h-0 flex-1 flex-col overflow-auto border-t border-default text-12">
      <div class="flex h-8 shrink-0 items-center gap-1.5 ps-3.5 pe-1.5 font-semibold text-toned">
        {{ rowText(row).kind }}<span class="grow" />
        <UButton
          label="Go to code"
          trailing-icon="i-lucide-arrow-up-right"
          variant="ghost"
          color="neutral"
          size="xs"
          @click="store.goTo(path, locationOf(row))"
        />
        <UTooltip v-if="row.kind !== 'pattern' && canEdit(locationOf(row))" text="Remove it from the file">
          <UButton
            icon="i-lucide-trash-2"
            aria-label="Remove"
            variant="ghost"
            color="error"
            size="xs"
            square
            @click="store.removeBlock(path, locationOf(row))"
          />
        </UTooltip>
      </div>
      <div class="grid grid-cols-[76px_minmax(0,1fr)] items-center gap-x-2.5 gap-y-1.5 px-3.5 pb-2">
        <template v-if="row.kind !== 'sub'">
          <span class="text-dimmed">Texture</span>
          <span class="flex min-w-0 items-center gap-2">
            <CoatField
              :workspace-id="workspaceId"
              :path="path"
              :field="row.kind === 'pattern' ? 'pattern' : 'texture'"
              :layer="layers[cardLayer]"
              :property="propertyOf(cardProperties, row.kind === 'pattern' ? 'pattern' : 'texture')"
              :is-read-only="isReadOnly(cardLayer, row.kind === 'pattern' ? 'pattern' : 'texture', cardProperties)"
            />
            <ReuseWriter :keys="[row.kind === 'pattern' ? 'pattern' : 'texture']" />
          </span>
        </template>
        <template v-else>
          <span class="text-dimmed">Parent</span>
          <span class="truncate font-mono text-11.5 text-highlighted">{{ layers[row.layers[0]].name }}</span>
        </template>
        <template v-if="layers[cardLayer]?.key !== 'textured_emblem'">
          <span class="text-dimmed">Colors</span>
          <span class="flex flex-wrap gap-1">
            <CoatField
              v-for="slot in SLOTS"
              :key="slot"
              :workspace-id="workspaceId"
              :path="path"
              :field="slot"
              :layer="layers[cardLayer]"
              :property="propertyOf(cardProperties, slot)"
              :is-read-only="isReadOnly(cardLayer, slot, cardProperties)"
            />
            <ReuseWriter :keys="SLOTS" />
          </span>
        </template>
        <template v-if="row.kind === 'emblem'">
          <span class="text-dimmed">Mask</span>
          <span class="flex min-w-0 items-center gap-2">
            <CoatField
              :workspace-id="workspaceId"
              :path="path"
              field="mask"
              :layer="layers[cardLayer]"
              :property="propertyOf(cardProperties, 'mask')"
              :is-read-only="isReadOnly(cardLayer, 'mask', cardProperties)"
            />
            <ReuseWriter :keys="['mask']" />
          </span>
        </template>
      </div>
      <!-- the instances: the selected one's cells are its inputs; where depth orders the coat, each one's place -->
      <template v-if="row.kind !== 'pattern'">
        <div
          class="flex h-8 shrink-0 items-center gap-1.5 border-t border-default ps-3.5 pe-1.5 font-semibold text-toned"
        >
          Instances<span class="grow" />
          <UButton
            v-if="row.kind === 'emblem' && canEdit(locationOf(row))"
            label="Add"
            icon="i-lucide-plus"
            variant="ghost"
            color="neutral"
            size="xs"
            @click="addInstance(row)"
          />
        </div>
        <table class="mx-2 mb-2 w-[calc(100%-1rem)] table-fixed font-mono text-11.5">
          <!-- each head over its inputs' text, the number column centered -->
          <thead class="font-sans text-11 text-dimmed">
            <tr class="h-5.5">
              <th class="w-7 font-normal">#</th>
              <template v-if="row.kind === 'emblem'">
                <th class="ps-2 text-start font-normal">Position</th>
                <th class="ps-2 text-start font-normal">Scale</th>
                <th class="w-11 ps-2 text-start font-normal">Turn</th>
                <th class="w-11 ps-2 text-start font-normal">Depth</th>
                <th v-if="isDepthOrdered" class="w-12 ps-2 text-start font-normal" title="Its place from the back">
                  Drawn
                </th>
              </template>
              <template v-else>
                <th class="ps-2 text-start font-normal">Offset</th>
                <th class="ps-2 text-start font-normal">Scale</th>
              </template>
              <th class="w-6" />
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(index, ordinal) in instances"
              :key="index"
              class="h-6.5 cursor-pointer text-toned"
              :class="index === instance && 'bg-(--accent-soft) text-highlighted'"
              @click="file.selection = index"
              @mouseenter="point(index)"
            >
              <td class="text-center text-dimmed">{{ ordinal + 1 }}</td>
              <td
                v-for="key in row.kind === 'emblem' ? ['position', 'scale', 'rotation', 'depth'] : ['offset', 'scale']"
                :key="key"
                class="pe-1"
              >
                <CoatField
                  v-if="index === instance && canEdit(layers[index].location)"
                  :workspace-id="workspaceId"
                  :path="path"
                  :field="key"
                  :layer="layers[index]"
                  :property="propertyOf(instanceProperties, key)"
                  :is-read-only="
                    isReadOnly(index, key, instanceProperties) ||
                    (row.kind === 'sub' && !propertyOf(instanceProperties, key))
                  "
                />
                <span v-else class="block truncate ps-2">{{ layers[index].properties?.[key] ?? "" }}</span>
              </td>
              <td v-if="row.kind === 'emblem' && isDepthOrdered" class="ps-2">
                {{ ownInstances.indexOf(index) + 1 || "" }}
              </td>
              <td>
                <UButton
                  v-if="layers[index].key === 'instance' && canEdit(layers[index].location)"
                  icon="i-lucide-x"
                  aria-label="Remove"
                  variant="ghost"
                  color="error"
                  size="xs"
                  square
                  @click.stop="store.removeBlock(path, layers[index].location)"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>
    <UEmpty
      v-else
      icon="i-lucide-mouse-pointer-click"
      title="Select a layer"
      description="Click it in the list or the picture, or put the caret in its code."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </div>
</template>
