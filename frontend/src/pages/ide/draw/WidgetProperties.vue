<!--
  The Properties tab of a window's panel: the selected widget, the pictures it may draw where it lists them, and one
  table of its properties with where each comes from, a row opening an editor that writes it on the widget.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { computed, shallowRef, watch } from "vue";

import * as draw from "@services/draw/service";
import type { PictureList, Property, Widget, Window } from "@services/draw/models";

import { toSentence } from "../../../lib/format";
import { picturesQuery, statesQuery } from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useWorkspaceStore } from "../../../stores/workspace";
import { applyEdits, openModel } from "../editor/models";
import ChoiceGallery from "./ChoiceGallery.vue";
import type { GalleryChoice } from "./ChoiceGallery.vue";
import { listProperties, TYPE_ICONS } from "./picture";

const { path, window } = defineProps<{ path: string; window: Window }>();

// the list a widget with no pictures queries, which the query never runs for
const NO_PICTURES: PictureList = {
  key: "",
  database: "",
  name: "",
  type: "",
  field: "",
  argument: "",
  folder: "",
  shown: "",
};
// a color as a GUI file writes one: red, green, blue and an optional alpha, each from 0 to 1, in braces
const COLOR = /^\{\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s+([\d.]+))?\s*\}$/;
const CHANNELS = ["Red", "Green", "Blue", "Alpha"];

const store = useDrawStore();
const workspace = useWorkspaceStore();

const file = computed(() => store.file(path));
const widgets = computed(() => window.widgets ?? []);
const places = computed(() => window.places ?? []);
const widget = computed(() => widgets.value[file.value.selected ?? -1]);
// whether the widget's file is read only, where a property's editor shows its value alone
const isReadOnly = computed(() => !!places.value[widget.value?.place ?? -1]?.readOnly);
const crumbs = computed(() => {
  const chain: Widget[] = [];
  for (let step: Widget | undefined = widget.value; step; step = step.id ? widgets.value[step.parent] : undefined) {
    chain.unshift(step);
  }
  return chain;
});

const { data: states } = useQuery(() => ({
  ...statesQuery({ path, name: window.name, picks: store.picks }),
  enabled: workspace.isOpen() && !!widget.value?.pictures,
}));
const pictureGroup = computed(() =>
  window.groups?.find((group) => group.pictures && group.readers?.includes(widget.value?.id ?? -1)),
);
const { data: pictures } = useQuery(() => ({
  ...picturesQuery({ path, list: widget.value?.pictures ?? NO_PICTURES }),
  enabled: workspace.isOpen() && !!widget.value?.pictures,
}));
const picture = computed(
  () => states.value?.values?.[pictureGroup.value?.id ?? ""] || widget.value?.pictures?.shown || "",
);
const pictureChoices = computed<GalleryChoice[]>(() => {
  const listed = (pictures.value ?? []).map(({ label, path: file, value }) => ({ value, label, path: file }));
  const key = widget.value?.pictures?.key ?? "";
  const shown = widget.value?.textures?.[key] ?? "";
  return listed.length ? listed : [{ value: picture.value, label: toFileLabel(picture.value), path: shown }];
});

const filter = shallowRef("");
const scope = shallowRef<"all" | "here">("all");
const properties = computed(() => listProperties(window, widget.value?.properties ?? null));
const rows = computed(() =>
  properties.value
    .map((property, ordinal) => ({ property, ordinal }))
    .filter(({ property }) => scope.value === "all" || property.origin === "widget")
    .filter(({ property }) => `${property.key} ${property.value}`.toLowerCase().includes(filter.value.toLowerCase())),
);
const openRow = shallowRef<number | null>(null);
const draft = shallowRef("");
watch(widget, () => (openRow.value = null));

const { mutate: write } = useMutation({
  mutation: async ({ key, value }: { key: string; value: string }) => {
    const selected = widget.value;
    if (!selected) return;
    const place = places.value[selected.place];
    applyEdits(await openModel(place), (await draw.EditProperty(place.path, selected.range, key, value)) ?? []);
  },
});

/** Opens a property's editor with its value, or closes it when it is open. */
function open(ordinal: number, property: Property): void {
  openRow.value = openRow.value === ordinal ? null : ordinal;
  draft.value = property.value;
}

/** Says where a property comes from: the widget, a template or a type, by its name, or that no file defines it. */
function toSource(property: Property): string {
  if (property.origin === "missing") return "missing";
  if (property.origin === "type") return `type ${property.writer}`;
  if (property.origin === "template") return property.writer;
  return property.writer ? `in ${property.writer}` : "this widget";
}

/** Splits a color as a GUI file writes it into its four channels, or returns null for any other value. */
function splitColor(value: string): string[] | null {
  const parts = COLOR.exec(value.trim());
  return parts ? [parts[1], parts[2], parts[3], parts[4] ?? "1"] : null;
}

/** Converts a color's channels, each from 0 to 1, to CSS. */
function toCss(channels: string[]): string {
  const [red, green, blue, alpha] = channels.map(Number);
  return `color(srgb ${red} ${green} ${blue} / ${alpha})`;
}

/** Sets one channel of the color in the editor. */
function setChannel(position: number, text: string): void {
  const channels = splitColor(draft.value) ?? ["0", "0", "0", "1"];
  channels[position] = text;
  draft.value = `{ ${channels.join(" ")} }`;
}

/** Converts an image file to its label: its name without its folder or extension. */
function toFileLabel(file: string): string {
  return (
    file
      .split(/[\\/]/)
      .pop()
      ?.replace(/\.[^.]+$/, "") ?? file
  );
}
</script>

<template>
  <div v-if="widget" class="flex min-h-0 flex-1 flex-col overflow-auto">
    <div class="flex shrink-0 flex-col gap-1.5 px-4 pt-3 pb-2.5">
      <nav class="flex items-center gap-0.5 overflow-x-auto font-mono text-11 whitespace-nowrap text-dimmed">
        <template v-for="(crumb, ordinal) in crumbs" :key="crumb.id">
          <UIcon v-if="ordinal" name="i-lucide-chevron-right" class="size-3 shrink-0" />
          <button
            type="button"
            class="flex shrink-0 items-center gap-1 rounded px-1 py-0.5 hover:bg-elevated"
            :class="crumb.id === widget.id ? 'text-highlighted' : 'hover:text-toned'"
            @click="store.select(path, crumb.id)"
            @mouseenter="store.hover(path, crumb.id)"
            @mouseleave="store.hover(path, null)"
          >
            <UIcon :name="TYPE_ICONS[crumb.type] ?? 'i-lucide-square-dashed'" class="size-3 shrink-0" />
            {{ crumb.name || crumb.type }}
          </button>
        </template>
      </nav>
      <div class="flex items-center gap-2">
        <span class="min-w-0 truncate font-mono text-14 font-semibold text-highlighted">
          {{ widget.name || widget.type }}
        </span>
        <span v-if="widget.name" class="shrink-0 text-12 text-dimmed">{{ widget.type }}</span>
        <span class="grow" />
        <UButton
          :label="`${places[widget.place]?.rel.split('/').pop()}:${widget.range.startLineNumber}`"
          trailing-icon="i-lucide-arrow-up-right"
          variant="ghost"
          color="neutral"
          size="xs"
          class="shrink-0 font-mono"
          @click="store.showCode(places[widget.place], widget.range)"
        />
        <USwitch
          :model-value="!file.hidden.includes(widget.id)"
          size="xs"
          aria-label="Shown"
          class="shrink-0"
          @update:model-value="store.hide(path, widget.id, !file.hidden.includes(widget.id))"
        />
      </div>
    </div>
    <div v-if="widget.pictures" class="shrink-0 border-t border-default px-4 py-3">
      <ChoiceGallery
        :choices="pictureChoices"
        :value="picture"
        :title="toSentence(pictureGroup?.label ?? 'pictures')"
        shape="wide"
        :is-picked="!!pictureGroup && pictureGroup.id in store.picks"
        @pick="(value) => pictureGroup && store.pick({ [pictureGroup.id]: value })"
        @reset="pictureGroup && store.unpick([pictureGroup.id])"
      />
    </div>
    <div class="flex shrink-0 items-center gap-2 border-t border-default px-3 py-2">
      <UInput v-model="filter" icon="i-lucide-search" size="sm" placeholder="Filter properties" class="grow" />
      <UFieldGroup size="sm">
        <UButton
          :label="`All ${properties.length}`"
          color="neutral"
          :variant="scope === 'all' ? 'soft' : 'outline'"
          @click="scope = 'all'"
        />
        <UButton
          :label="`Written here ${properties.filter(({ origin }) => origin === 'widget').length}`"
          color="neutral"
          :variant="scope === 'here' ? 'soft' : 'outline'"
          @click="scope = 'here'"
        />
      </UFieldGroup>
    </div>
    <div
      class="sticky top-0 grid shrink-0 grid-cols-[minmax(0,10rem)_minmax(0,1fr)_minmax(0,8rem)] border-y border-default bg-muted px-4 text-11 leading-7 font-medium text-dimmed"
    >
      <span>Property</span><span class="ps-3">Value</span><span class="ps-3">From</span>
    </div>
    <template v-for="{ property, ordinal } in rows" :key="ordinal">
      <button
        type="button"
        class="grid min-h-7 shrink-0 grid-cols-[minmax(0,10rem)_minmax(0,1fr)_minmax(0,8rem)] items-center border-b border-default px-4 text-start text-12 hover:bg-elevated"
        :class="openRow === ordinal && 'bg-(--accent-soft)'"
        @click="open(ordinal, property)"
      >
        <span class="truncate font-mono text-toned" :title="property.key">{{ property.key }}</span>
        <span class="flex min-w-0 items-center gap-1.5 ps-3 font-mono text-highlighted">
          <span
            v-if="splitColor(property.value)"
            class="size-3 shrink-0 rounded-sm ring-1 ring-default"
            :style="{ background: toCss(splitColor(property.value) ?? []) }"
          />
          <span class="truncate" :title="property.value">{{ property.value }}</span>
        </span>
        <span
          class="truncate ps-3 font-mono text-11"
          :class="property.origin === 'missing' ? 'text-warning' : 'text-dimmed'"
          :title="`${places[property.place]?.rel}:${property.range.startLineNumber}`"
          >{{ toSource(property) }}</span
        >
      </button>
      <div
        v-if="openRow === ordinal"
        class="flex shrink-0 flex-col gap-2.5 border-b border-default bg-elevated px-4 py-3"
      >
        <span v-if="isReadOnly" class="font-mono text-12 break-all text-highlighted">{{ property.value }}</span>
        <div v-else-if="splitColor(draft)" class="flex items-end gap-2">
          <span
            class="size-10 shrink-0 rounded-md ring-1 ring-default"
            :style="{ background: toCss(splitColor(draft) ?? []) }"
          />
          <UFormField
            v-for="(channel, position) in CHANNELS"
            :key="channel"
            :label="channel"
            size="xs"
            class="min-w-0 grow"
          >
            <UInput
              :model-value="splitColor(draft)?.[position]"
              type="number"
              step="0.01"
              min="0"
              max="1"
              size="xs"
              class="w-full font-mono"
              @update:model-value="(text) => setChannel(position, String(text))"
            />
          </UFormField>
        </div>
        <UTextarea v-else v-model="draft" :rows="1" autoresize size="sm" class="font-mono" />
        <span class="text-12 text-dimmed">
          {{
            property.origin === "widget" && !property.writer ? "Written in this widget" : `From ${toSource(property)}`
          }},
          <button
            type="button"
            class="font-mono text-(--link) hover:underline"
            @click="store.showCode(places[property.place], property.range)"
          >
            {{ places[property.place]?.rel }}:{{ property.range.startLineNumber }}
          </button>
        </span>
        <div class="flex items-center gap-2">
          <UButton
            v-if="!isReadOnly"
            :label="property.origin === 'widget' ? 'Save' : 'Set it on this widget'"
            size="xs"
            :disabled="draft === property.value && property.origin === 'widget'"
            @click="write({ key: property.key, value: draft })"
          />
          <UButton
            label="Go to code"
            variant="ghost"
            color="neutral"
            size="xs"
            @click="store.showCode(places[property.place], property.range)"
          />
          <span v-if="isReadOnly" class="text-11 text-dimmed">This file is read only.</span>
        </div>
      </div>
    </template>
  </div>
  <UEmpty
    v-else
    icon="i-lucide-mouse-pointer-click"
    title="Select a widget"
    description="Click it in the picture, or put the caret in its code."
    variant="naked"
    size="sm"
    class="flex-1"
  />
</template>
