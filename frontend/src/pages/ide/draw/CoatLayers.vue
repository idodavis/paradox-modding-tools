<!--
  The Layers panel of a coat: a sortable list of layers, then the selected layer's texture, colors, mask and instances.
  Every change is an edit to the file.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate } from "@vueuse/core";
import { insertNodeAt, removeNode, useSortable } from "@vueuse/integrations/useSortable";
import type { IRange } from "monaco-editor";
import type { SortableEvent } from "sortablejs";
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import type { ComputedRef, ShallowRef } from "vue";

import * as draw from "@services/draw/service";
import * as lsp from "@services/lsp/service";
import type { Coat, Instance, Layer } from "@services/draw/models";
import type { Edit } from "@services/lsp/models";

import { plural, toSentence } from "../../../lib/format";
import { colorsQuery, texturesQuery } from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useWorkspaceStore } from "../../../stores/workspace";
import ChoiceGallery from "../../../ui/ChoiceGallery.vue";
import type { GalleryChoice } from "../../../ui/ChoiceGallery.vue";
import ValueField from "../../../ui/ValueField.vue";
import { applyEdits } from "../editor/models";
import type { Model } from "../editor/models";
import { drawTile, isOwn, toRgba } from "./coat";
import type { Rgba } from "./coat";

const { model, coat } = defineProps<{ model: Model; coat: Coat }>();

/** A Row is one layer of the list with its index among the coat's layers. */
type Row = { layer: Layer; index: number };

const SORTING = { handle: ".pmt-grip", animation: 150, watchElement: true };
// The instance columns, in order. One whose input is a pair holds two
// numbers, across and down, that can be locked.
const COLUMNS = [
  { key: "position", title: "Position" },
  { key: "offset", title: "Offset" },
  { key: "scale", title: "Scale" },
  { key: "rotation", title: "Turn" },
  { key: "depth", title: "Depth" },
];

const store = useDrawStore();
const workspace = useWorkspaceStore();
const file = computed(() => store.file(model.place.path));
const { data: colors } = useQuery(() => ({ ...colorsQuery, enabled: workspace.isOpen() }));

const rows = computed<Row[]>(() => (coat.layers ?? []).map((layer, index) => ({ layer, index })));
const pattern = computed(() => rows.value.find(({ layer }) => layer.role === "pattern"));
const emblems = computed(() => rows.value.filter(({ layer }) => layer.emblem));
const subs = computed(() => rows.value.filter(({ layer }) => layer.role === "sub"));
const current = computed(() => rows.value.find(({ index }) => index === file.value.selected));
const batchRows = computed(() => rows.value.filter(({ index }) => file.value.batch.includes(index)));
const isAnyEditable = computed(() => batchRows.value.some(({ layer }) => canEdit(layer)));
const columns = computed(() => COLUMNS.filter(({ key }) => current.value?.layer.inputs?.[key]));
const isDepthOrdered = computed(() => emblems.value.some(({ layer }) => layer.instances?.some(({ depth }) => depth)));
const isDrawnShown = computed(
  () => isDepthOrdered.value && emblems.value.some(({ index }) => index === current.value?.index),
);
const addMenu = [
  { label: "Emblem", icon: "i-lucide-sticker", role: "colored_emblem" },
  { label: "Textured emblem", icon: "i-lucide-image", role: "textured_emblem" },
  { label: "Sub", icon: "i-lucide-shield-half", role: "sub" },
].map(({ label, icon, role }) => ({ label, icon, onSelect: () => add(role) }));

const { data: textures } = useQuery(() => ({
  ...texturesQuery(current.value?.layer.role ?? ""),
  enabled: workspace.isOpen() && listKeys("texture").length > 0,
}));
const textureChoices = computed<GalleryChoice[]>(() =>
  (textures.value ?? []).map(({ label, place }) => ({ value: label, label, path: place.path })),
);
// The key of the color slot the gallery picks for. The first slot while empty.
const slot = shallowRef("");
// The keys whose two numbers change together.
const locks = shallowRef(new Set<string>());
const slotKey = computed(() => (listKeys("color").includes(slot.value) ? slot.value : (listKeys("color")[0] ?? "")));
const colorChoices = computed<GalleryChoice[]>(() => {
  const named = (colors.value ?? []).map(({ name, css }) => ({ value: name, label: name, color: css }));
  const layer = current.value?.layer;
  const custom = layer && !findColorName(layer, slotKey.value) ? findColor(layer, slotKey.value) : "";
  return custom ? [...named, { value: custom, label: "Custom", color: custom }] : named;
});
watch(
  () => current.value?.index,
  () => (slot.value = ""),
);

const [DefineRow, ReuseRow] = createReusableTemplate<{ row: Row }>();

const { mutate: edit } = useMutation({
  mutation: async (request: () => Promise<Edit[] | null>) => applyEdits(model, (await request()) ?? []),
});

/** canEdit reports whether a layer may be changed. */
const canEdit = (layer: Layer) => isOwn(coat, layer, model.place.key);

/** listKeys lists the keys of the selected layer that the panel edits as the given kind of input. */
function listKeys(input: string): string[] {
  return Object.entries(current.value?.layer.inputs ?? {})
    .filter(([, kind]) => kind === input)
    .map(([key]) => key);
}

/** findSlot returns the index in a layer's colors of the slot a key writes, or 0 if the key is no slot. */
const findSlot = (key: string) => (coat.slots ?? []).indexOf(key) + 1;

/** findColor returns the color a layer has in a slot, as CSS, or "" if it has none. */
const findColor = (layer: Layer, key: string) => layer.colors?.[findSlot(key)] ?? "";

/** findColorName returns the name of the game's color a layer has in a slot, or "" if it has none. */
const findColorName = (layer: Layer, key: string) => layer.colorNames?.[findSlot(key)] ?? "";

/** findNumbers returns the numbers an instance has for a key. */
function findNumbers(instance: Instance, key: string): number[] {
  if (key === "scale") return instance.scale;
  return key === "rotation" || key === "depth" ? [instance[key]] : instance.position;
}

/** findBlock returns the range of the block that writes a layer. */
const findBlock = (layer: Layer) => (layer.role === "pattern" ? coat.range : layer.range);

/** writeLayer writes one key of the block that writes the selected layer. */
function writeLayer(key: string, text: string): void {
  const layer = current.value?.layer;
  if (layer) edit(() => draw.EditProperties(model.place.path, [{ block: findBlock(layer), key, value: text }]));
}

/** writeQuoted writes a name or a file of the selected layer in quotes. */
function writeQuoted(key: string, text: string): void {
  writeLayer(key, `"${text}"`);
}

/** writeColor writes a color picked as #rrggbb in one slot of the selected layer. */
function writeColor(key: string, hex: string): void {
  const layer = current.value?.layer;
  if (!layer) return;
  edit(async () =>
    draw.EditProperties(model.place.path, [
      { block: findBlock(layer), key, value: await presentColor(layer, toRgba(hex)) },
    ]),
  );
}

/** toCoatColor converts a layer's color in a slot to how a coat writes it. It returns "" for a slot with no color. */
async function toCoatColor(layer: Layer, key: string): Promise<string> {
  const name = findColorName(layer, key);
  const color = findColor(layer, key);
  if (name || !color) return name && `"${name}"`;
  return presentColor(layer, toRgba(color));
}

/** presentColor returns the text to write for a custom color in the block of a layer. */
function presentColor(layer: Layer, [red, green, blue, alpha]: Rgba): Promise<string> {
  return lsp.PresentColor(model.place.path, findBlock(layer), { r: red, g: green, b: blue, a: alpha });
}

/** copyColors copies a layer's colors for pasting onto other layers. */
async function copyColors(layer: Layer): Promise<void> {
  const keys = Object.entries(layer.inputs ?? {}).flatMap(([key, input]) => (input === "color" ? [key] : []));
  const texts = await Promise.all(keys.map((key) => toCoatColor(layer, key)));
  store.colors = Object.fromEntries(keys.flatMap((key, position) => (texts[position] ? [[key, texts[position]]] : [])));
}

/** pasteColors pastes the copied colors onto the given layers as one edit. */
function pasteColors(layers: Layer[]): void {
  const settings = layers.filter(canEdit).flatMap((layer) =>
    Object.entries(store.colors)
      .filter(([key]) => layer.inputs?.[key] === "color")
      .map(([key, value]) => ({ block: findBlock(layer), key, value })),
  );
  edit(() => draw.EditProperties(model.place.path, settings));
}

/** writeRegions writes the regions the selected layer's mask keeps. At least one stays. */
function writeRegions(regions: string): void {
  if (regions) writeLayer("mask", `{ ${regions} }`);
}

/** writeInstance writes an instance's numbers for a key. An instance not yet written is added. */
function writeInstance(instance: Instance, key: string, axis: number, number: number): void {
  if (!instance.range.startLineNumber) return addInstance();
  const before = findNumbers(instance, key);
  const numbers = locks.value.has(key)
    ? changeTogether(key, before, axis, number)
    : before.map((existing, position) => (position === axis ? number : existing));
  const value = numbers.length > 1 ? `{ ${numbers.join(" ")} }` : String(numbers[0]);
  edit(() => draw.EditProperties(model.place.path, [{ block: instance.range, key, value }]));
}

/**
 * changeTogether returns a pair of numbers with the one on the given axis set and the other changed to match. A scale
 * keeps the ratio between them, so the layer keeps its shape; a position or an offset moves both by the same step.
 */
function changeTogether(key: string, numbers: number[], axis: number, number: number): number[] {
  const [mine = 0, other = 0] = axis === 0 ? numbers : numbers.toReversed();
  const follower = key === "scale" ? (mine ? (other * number) / mine : number) : other + number - mine;
  const pair = [number, Number(follower.toFixed(6))];
  return axis === 0 ? pair : pair.toReversed();
}

/** toggleLock locks a key's two numbers to change together, or unlocks them. */
function toggleLock(key: string): void {
  const next = new Set(locks.value);
  if (!next.delete(key)) next.add(key);
  locks.value = next;
}

/** add adds a layer of the given role after the coat's last layer of its kind. */
function add(role: string): void {
  const layers = coat.layers ?? [];
  const position = role === "sub" ? layers.length : layers.findLastIndex((layer) => layer.role !== "sub") + 1;
  edit(() => draw.AddLayer(model.place.path, coat.name, role, position));
}

/** addInstance adds an instance to the selected layer. */
function addInstance(): void {
  const layer = current.value?.layer;
  if (layer) edit(() => draw.AddInstance(model.place.path, layer.range));
}

/** remove removes the block of a layer or an instance from the file. */
function remove(range: IRange): void {
  edit(() => draw.RemoveBlock(model.place.path, range));
}

/** removeBatch removes the checked layers from the file, except the pattern and those written elsewhere. */
function removeBatch(): void {
  const ranges = batchRows.value
    .filter(({ layer }) => layer.role !== "pattern" && canEdit(layer))
    .map(({ layer }) => layer.range);
  edit(async () =>
    (await Promise.all(ranges.map((range) => draw.RemoveBlock(model.place.path, range)))).flatMap(
      (edits) => edits ?? [],
    ),
  );
  file.value.batch = [];
}

/** hideBatch hides the checked layers for the session. */
function hideBatch(): void {
  for (const { index } of batchRows.value) store.hide(model.place.path, index, true);
}

/** check checks a layer for acting on several, or unchecks it. */
function check(index: number, isChecked: boolean): void {
  file.value.batch = isChecked ? [...file.value.batch, index] : file.value.batch.filter((other) => other !== index);
}

/** drop moves the layer of a dragged row to where it was dropped. */
function drop(list: Row[], event: SortableEvent): void {
  // The edit moves the layer, so the row goes back where it was.
  removeNode(event.item);
  insertNodeAt(event.from, event.item, event.oldIndex ?? 0);
  const source = list[event.oldIndex ?? 0];
  const target = list[event.newIndex ?? 0];
  if (!source || !target || source.index === target.index) return;
  if (!canEdit(source.layer) || !canEdit(target.layer)) return;
  edit(() => draw.MoveLayer(model.place.path, coat.name, source.index, target.index));
}

/** sort makes the rows of a list sortable by their grips. */
function sort(list: Readonly<ShallowRef<HTMLElement | null>>, listed: ComputedRef<Row[]>): void {
  useSortable(list, listed, { ...SORTING, onUpdate: (event: SortableEvent) => drop(listed.value, event) });
}
sort(useTemplateRef<HTMLElement>("emblemList"), emblems);
sort(useTemplateRef<HTMLElement>("subList"), subs);
</script>

<template>
  <DefineRow v-slot="{ row }">
    <div
      class="flex h-9 min-w-0 cursor-pointer items-center gap-2 ps-2 pe-2.5 text-12 whitespace-nowrap text-toned"
      :class="[
        row.index === file.selected && 'bg-(--accent-soft) text-highlighted shadow-[inset_2px_0_0_var(--accent)]',
        file.hidden.includes(row.index) && 'opacity-50',
      ]"
      @click="store.select(model.place.path, row.index)"
      @mouseenter="store.hover(model.place.path, row.index)"
    >
      <UIcon
        name="i-lucide-grip-vertical"
        class="pmt-grip size-3.5 shrink-0 text-dimmed"
        :class="row.layer.role === 'pattern' || !canEdit(row.layer) ? 'invisible' : 'cursor-grab'"
      />
      <UButton
        :icon="file.hidden.includes(row.index) ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :aria-label="file.hidden.includes(row.index) ? 'Show' : 'Hide'"
        variant="link"
        color="neutral"
        size="xs"
        square
        class="p-0"
        @click.stop="store.hide(model.place.path, row.index, !file.hidden.includes(row.index))"
      />
      <span class="flex" @click.stop>
        <UCheckbox
          :model-value="file.batch.includes(row.index)"
          :aria-label="`Check ${row.layer.texture}`"
          @update:model-value="(isChecked) => check(row.index, isChecked === true)"
        />
      </span>
      <span
        class="flex size-6.5 shrink-0 items-center justify-center overflow-hidden rounded bg-(--bg-0) ring-1 ring-default"
      >
        <canvas
          v-if="row.layer.path"
          :key="`${row.layer.path}|${row.layer.colors?.join()}`"
          :ref="(canvas) => void drawTile(canvas, row.layer.path, row.layer.recolor ?? null)"
          class="size-full"
        />
      </span>
      <span class="min-w-0 truncate font-mono text-11.5 text-highlighted" :title="row.layer.texture">{{
        row.layer.texture.replace(/\.[^.]+$/, "")
      }}</span>
      <span class="ms-auto shrink-0 text-11 text-dimmed">
        {{ toSentence(row.layer.role.replaceAll("_", " ")) }}
        <template v-if="row.layer.role !== 'pattern'">
          · {{ plural(row.layer.instances?.length ?? 0, "instance") }}
        </template>
      </span>
    </div>
  </DefineRow>
  <div class="flex min-h-0 flex-1 flex-col overflow-auto" @mouseleave="store.hover(model.place.path, null)">
    <div class="flex h-9 shrink-0 items-center gap-1.5 ps-3.5 pe-1.5 text-12 font-semibold">
      {{ plural(rows.length, "layer") }}<span class="grow" />
      <UDropdownMenu v-if="!model.place.readOnly" :items="addMenu">
        <UButton icon="i-lucide-plus" label="Add" variant="ghost" color="neutral" size="xs" />
      </UDropdownMenu>
    </div>
    <div class="mx-3 shrink-0 overflow-hidden rounded-lg border border-default bg-default py-1">
      <ReuseRow v-if="pattern" :row="pattern" />
      <div ref="emblemList"><ReuseRow v-for="row in emblems" :key="row.index" :row="row" /></div>
      <div ref="subList"><ReuseRow v-for="row in subs" :key="row.index" :row="row" /></div>
      <p v-if="isDepthOrdered" class="px-3.5 pt-1.5 pb-1 text-11 text-dimmed">
        Depth orders this coat: a larger depth is drawn behind. A drop changes the order among equal depths.
      </p>
    </div>
    <div
      v-if="batchRows.length"
      class="mx-3 mt-2 flex shrink-0 items-center gap-1 rounded-lg bg-elevated py-1 ps-3 pe-1 text-12"
    >
      <span class="font-semibold">{{ plural(batchRows.length, "layer") }}</span>
      <span class="grow" />
      <UButton
        label="Copy colors"
        icon="i-lucide-copy"
        variant="ghost"
        color="neutral"
        size="xs"
        @click="copyColors((current ?? batchRows[0]).layer)"
      />
      <UButton
        label="Paste colors"
        icon="i-lucide-clipboard-paste"
        variant="ghost"
        color="neutral"
        size="xs"
        :disabled="!Object.keys(store.colors).length || !isAnyEditable"
        @click="pasteColors(batchRows.map(({ layer }) => layer))"
      />
      <UButton label="Hide" icon="i-lucide-eye-off" variant="ghost" color="neutral" size="xs" @click="hideBatch" />
      <UTooltip text="Remove the checked layers from the file">
        <UButton
          icon="i-lucide-trash-2"
          aria-label="Remove"
          variant="ghost"
          color="error"
          size="xs"
          square
          :disabled="!isAnyEditable"
          @click="removeBatch"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-x"
        aria-label="Uncheck all"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        @click="file.batch = []"
      />
    </div>
    <div v-if="current" class="mt-2 flex shrink-0 flex-col border-t border-default text-12">
      <div class="flex h-9 shrink-0 items-center gap-1.5 ps-3.5 pe-1.5 font-semibold text-toned">
        {{ toSentence(current.layer.role.replaceAll("_", " ")) }}<span class="grow" />
        <UButton
          v-if="current.layer.writer"
          :label="`from ${current.layer.writer}`"
          variant="link"
          color="neutral"
          size="xs"
          class="p-0 text-11 font-normal text-dimmed normal-case"
          @click="store.showCode(current.layer.place, current.layer.range)"
        />
        <UButton
          label="Go to code"
          trailing-icon="i-lucide-arrow-up-right"
          variant="ghost"
          color="neutral"
          size="xs"
          @click="store.showCode(current.layer.place, current.layer.range)"
        />
        <UTooltip v-if="current.layer.role !== 'pattern' && canEdit(current.layer)" text="Remove it from the file">
          <UButton
            icon="i-lucide-trash-2"
            aria-label="Remove"
            variant="ghost"
            color="error"
            size="xs"
            square
            @click="remove(current.layer.range)"
          />
        </UTooltip>
      </div>
      <div v-if="current.layer.role === 'sub'" class="flex items-center gap-2.5 px-3.5 pb-2">
        <span class="w-16 shrink-0 text-dimmed">Parent</span>
        <span class="truncate font-mono text-11.5 text-highlighted">{{ current.layer.texture }}</span>
      </div>
      <div v-for="key in listKeys('texture')" :key="key" class="border-t border-default px-3.5 py-3">
        <ChoiceGallery
          :choices="textureChoices"
          :value="current.layer.texture"
          title="Texture"
          shape="square"
          :disabled="!canEdit(current.layer)"
          @pick="(texture) => writeQuoted(key, texture)"
        >
          <template #tile="{ choice }">
            <canvas
              :key="`${choice.path}|${current.layer.colors?.join()}`"
              :ref="(canvas) => void drawTile(canvas, choice.path ?? '', current?.layer.recolor ?? null)"
              class="size-full object-contain"
            />
          </template>
        </ChoiceGallery>
      </div>
      <div v-if="listKeys('color').length" class="flex flex-col gap-2.5 border-t border-default px-3.5 py-3">
        <div class="flex items-center gap-1.5">
          <UFieldGroup size="xs">
            <UButton
              v-for="(key, position) in listKeys('color')"
              :key="key"
              :label="`Color ${position + 1}`"
              color="neutral"
              :variant="key === slotKey ? 'soft' : 'outline'"
              @click="slot = key"
            >
              <template #leading>
                <span
                  class="size-3 rounded-sm ring-1 ring-default"
                  :style="{ background: findColor(current.layer, key) }"
                />
              </template>
            </UButton>
          </UFieldGroup>
          <span class="grow" />
          <UTooltip text="Copy this layer's colors">
            <UButton
              icon="i-lucide-copy"
              aria-label="Copy colors"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              @click="copyColors(current.layer)"
            />
          </UTooltip>
          <UTooltip text="Paste the copied colors onto this layer">
            <UButton
              icon="i-lucide-clipboard-paste"
              aria-label="Paste colors"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              :disabled="!Object.keys(store.colors).length || !canEdit(current.layer)"
              @click="pasteColors([current.layer])"
            />
          </UTooltip>
        </div>
        <ChoiceGallery
          :choices="colorChoices"
          :value="findColorName(current.layer, slotKey) || findColor(current.layer, slotKey)"
          :title="`Color ${listKeys('color').indexOf(slotKey) + 1}`"
          shape="swatch"
          :disabled="!canEdit(current.layer)"
          @pick="(color) => writeQuoted(slotKey, color)"
        >
          <template #extra>
            <UPopover>
              <button type="button" class="flex min-w-0 flex-col gap-1 text-start text-11 text-toned">
                <span class="flex h-7 w-full items-center justify-center rounded-md ring-1 ring-default">
                  <UIcon name="i-lucide-pipette" class="size-3.5" />
                </span>
                <span class="truncate">Custom</span>
              </button>
              <template #content>
                <UColorPicker
                  class="p-2"
                  :model-value="findColor(current.layer, slotKey)"
                  :throttle="250"
                  @update:model-value="(hex) => hex && writeColor(slotKey, hex)"
                />
              </template>
            </UPopover>
          </template>
        </ChoiceGallery>
      </div>
      <div v-if="listKeys('regions').length" class="flex items-center gap-2.5 border-t border-default px-3.5 py-2.5">
        <span class="w-16 shrink-0 text-dimmed">Mask</span>
        <UButton
          label="Whole field"
          size="xs"
          :color="current.layer.maskRange.startLineNumber ? 'neutral' : 'primary'"
          :variant="current.layer.maskRange.startLineNumber ? 'outline' : 'soft'"
          :disabled="!canEdit(current.layer) || !current.layer.maskRange.startLineNumber"
          @click="remove(current.layer.maskRange)"
        />
        <ValueField
          kind="toggles"
          label=""
          :choices="(coat.regions ?? []).map(String)"
          :value="(current.layer.regions ?? []).join(' ')"
          :disabled="!canEdit(current.layer)"
          @update:value="writeRegions"
        />
      </div>
      <div
        v-for="list in current.layer.lists"
        :key="list.id"
        class="flex items-center gap-2.5 border-t border-default px-3.5 py-2"
      >
        <span class="w-16 shrink-0 truncate text-dimmed" :title="list.label">{{ list.label }}</span>
        <ValueField
          kind="choice"
          label=""
          :choices="list.options ?? []"
          :value="list.picked ?? ''"
          class="min-w-0 grow"
          @update:value="(option) => store.pick({ [list.id]: option })"
        />
      </div>
      <template v-if="columns.length">
        <div
          class="flex h-8 shrink-0 items-center gap-1.5 border-t border-default ps-3.5 pe-1.5 font-semibold text-toned"
        >
          Instances<span class="grow" />
          <UButton
            v-if="canEdit(current.layer)"
            label="Add"
            icon="i-lucide-plus"
            variant="ghost"
            color="neutral"
            size="xs"
            @click="addInstance"
          />
        </div>
        <table class="mx-2 mb-2 w-[calc(100%-1rem)] table-fixed font-mono text-11.5">
          <thead class="font-sans text-11 text-dimmed">
            <tr class="h-5.5">
              <th class="w-7 font-normal">#</th>
              <th v-for="{ key, title } in columns" :key="key" class="ps-2 text-start font-normal">
                <span class="flex items-center gap-0.5">
                  {{ title }}
                  <UTooltip
                    v-if="current.layer.inputs?.[key] === 'pair'"
                    :text="locks.has(key) ? 'Both numbers change together' : 'Each number changes on its own'"
                  >
                    <UButton
                      :icon="locks.has(key) ? 'i-lucide-link' : 'i-lucide-unlink'"
                      :aria-label="`Lock the two numbers of ${title}`"
                      :aria-pressed="locks.has(key)"
                      :color="locks.has(key) ? 'primary' : 'neutral'"
                      variant="ghost"
                      size="xs"
                      square
                      class="p-0.5"
                      @click="toggleLock(key)"
                    />
                  </UTooltip>
                </span>
              </th>
              <th v-if="isDrawnShown" class="w-12 ps-2 text-start font-normal" title="Its place from the back">
                Drawn
              </th>
              <th class="w-6" />
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(instance, ordinal) in current.layer.instances ?? []"
              :key="ordinal"
              class="h-6.5 cursor-pointer text-toned"
              :class="file.instance === ordinal && 'bg-(--accent-soft) text-highlighted'"
              @click="store.select(model.place.path, current.index, ordinal)"
              @mouseenter="store.hover(model.place.path, current.index, ordinal)"
            >
              <td class="text-center text-dimmed">{{ ordinal + 1 }}</td>
              <td v-for="{ key } in columns" :key="key" class="pe-1">
                <span class="flex gap-1">
                  <ValueField
                    v-for="(number, axis) in findNumbers(instance, key)"
                    :key="axis"
                    kind="number"
                    label=""
                    :step="0.01"
                    :value="String(number)"
                    :disabled="!canEdit(current.layer)"
                    class="w-0 grow"
                    @update:value="(text) => writeInstance(instance, key, axis, Number(text))"
                  />
                </span>
              </td>
              <td v-if="isDrawnShown" class="ps-2">{{ instance.rank + 1 }}</td>
              <td>
                <UButton
                  v-if="canEdit(current.layer) && instance.range.startLineNumber"
                  icon="i-lucide-x"
                  aria-label="Remove"
                  variant="ghost"
                  color="error"
                  size="xs"
                  square
                  @click="remove(instance.range)"
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
      description="Click it in the list, or put the caret in its code."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </div>
</template>
