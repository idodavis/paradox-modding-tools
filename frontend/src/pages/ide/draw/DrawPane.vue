<!--
  The Visual view's pane: the picture on the viewport, the marks over it, and the panel, with the code beside it in
  Split.
-->
<script setup lang="ts">
import type { ContextMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useDebounceFn, useEventListener, useResizeObserver } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { Range } from "monaco-editor";
import { computed, nextTick, shallowRef, useTemplateRef, watch, watchEffect, watchPostEffect } from "vue";

import { Format } from "@internal/files/models";
import * as lsp from "@services/lsp/service";
import type { Widget, Window } from "@services/draw/models";
import type { Place } from "@services/workspace/models";

import {
  coatQuery,
  drawablesQuery,
  fileUrl,
  framesQuery,
  settingsCatalogQuery,
  stateQuery,
  statesQuery,
  windowQuery,
} from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useSettingsStore } from "../../../stores/settings";
import { useWorkspaceStore } from "../../../stores/workspace";
import CodeEditor from "../CodeEditor.vue";
import type { Model } from "../editor/models";
import CodeLink from "./CodeLink.vue";
import DrawDetail from "./DrawDetail.vue";
import PictureViewport from "./PictureViewport.vue";
import WidgetMarks from "./WidgetMarks.vue";
import { drawCoat, findBoxes } from "./coat";
import { buildPicture, readRects } from "./picture";
import type { PictureNode } from "./picture";

const { model, split } = defineProps<{ model: Model; split: boolean }>();

const NO_WINDOW: Window = { name: "", widgets: [], holders: [], datacontext: "", places: [], fonts: [] };

const store = useDrawStore();
const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const file = computed(() => store.file(model.place.path));
const isCoat = computed(() => model.place.format !== Format.FormatGUI);
const isBounded = shallowRef(false);

const viewport = useTemplateRef("viewport");
const floor = useTemplateRef<HTMLElement>("floor");
const canvas = useTemplateRef<HTMLCanvasElement>("canvas");
const nodes = shallowRef<PictureNode[]>([]);
const windowRects = shallowRef(new Map<number, DOMRect[]>());
const renderedMs = shallowRef(0);

const { data: state } = useQuery(() => ({ ...stateQuery, enabled: workspace.isOpen() }));
const { data: catalog } = useQuery(() => ({ ...settingsCatalogQuery, enabled: workspace.isOpen() }));
const { data: frames } = useQuery(() => ({ ...framesQuery, enabled: workspace.isOpen() }));
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(model.place.path),
  enabled: workspace.isOpen(),
}));

const resolutionKey = computed(() => `draw.resolution.${state.value?.game?.id ?? ""}`);
const resolution = computed(() => settings.value(resolutionKey.value));
const resolutions = computed(() => catalog.value?.find(({ key }) => key === resolutionKey.value)?.choices ?? []);
const screen = computed(() => {
  const [width = 0, height = 0] = resolution.value.split("x").map(Number);
  return { width, height };
});

const drawable = computed(() => {
  const list = drawables.value ?? [];
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  const shown = list.find(({ name }) => name === file.value.drawable);
  return (shown ?? list.findLast(({ range }) => Range.containsPosition(range, caret)) ?? list[0])?.name ?? "";
});
watchEffect(() => {
  if (drawable.value && file.value.drawable !== drawable.value) store.draw(model.place.path, drawable.value);
});

const {
  data: drawnWindow,
  error: windowError,
  asyncStatus: windowStatus,
  refetch: refetchWindow,
} = useQuery(() => ({
  ...windowQuery({ path: model.place.path, name: drawable.value }),
  enabled: !isCoat.value && !!drawable.value && !!resolution.value,
}));
const {
  data: drawnCoat,
  error: coatError,
  asyncStatus: coatStatus,
  refetch: refetchCoat,
} = useQuery(() => ({
  ...coatQuery({ path: model.place.path, name: drawable.value, picks: store.picks }),
  enabled: isCoat.value && !!drawable.value && !!resolution.value,
}));
const { data: states } = useQuery(() => ({
  ...statesQuery({ path: model.place.path, name: drawnWindow.value?.name ?? "", picks: store.picks }),
  enabled: workspace.isOpen() && !!drawnWindow.value,
}));
const asyncStatus = computed(() => (isCoat.value ? coatStatus : windowStatus).value);
const error = computed(() => (isCoat.value ? coatError : windowError).value);
const refetch = () => (isCoat.value ? refetchCoat : refetchWindow)();

const hidden = computed(() => new Set([...file.value.hidden, ...(states.value?.hidden ?? [])]));
const boxes = computed(() => (drawnCoat.value ? findBoxes(drawnCoat.value, screen.value.height) : windowRects.value));
const rects = computed(() => {
  const first = new Map([...boxes.value].flatMap(([id, list]) => (list[0] ? [[id, list[0]] as const] : [])));
  const { selected, instance, hovered, hoveredInstance } = file.value;
  for (const [id, index] of [
    [hovered, hoveredInstance],
    [selected, instance],
  ] as const) {
    const box = id === null || index === null ? undefined : boxes.value.get(id)?.[index];
    if (id !== null && box) first.set(id, box);
  }
  return first;
});
const hasPicture = computed(() => !!(drawnWindow.value || drawnCoat.value));
const isEmpty = computed(() => !!drawnWindow.value && windowRects.value.size > 0 && !rects.value.get(0)?.width);
const markBoxes = computed(() =>
  drawnCoat.value ? new Map([...rects.value].map(([id, rect]) => [id, [rect]])) : boxes.value,
);
const blocks = computed(() => {
  const window = drawnWindow.value;
  if (window) {
    const places = window.places ?? [];
    return (window.widgets ?? []).map(({ id, parent, place, range }) => ({ id, parent, place: places[place], range }));
  }
  return drawnCoat.value?.layers?.map(({ place, range }, id) => ({ id, parent: -1, place, range })) ?? [];
});
const missing = computed(() =>
  (drawnWindow.value?.widgets ?? []).flatMap((widget) =>
    (widget.properties ?? [])
      .filter((property) => property.origin === "missing")
      .map((property) => ({
        id: widget.id,
        text: `${property.key} ${property.value}`,
        detail: widget.name || widget.type,
      })),
  ),
);
const lists = computed(() => [
  {
    label: "missing",
    icon: "i-lucide-triangle-alert",
    color: "warning" as const,
    isHidden: false,
    rows: missing.value,
  },
  {
    label: "hidden",
    icon: "i-lucide-eye-off",
    color: "neutral" as const,
    isHidden: true,
    rows: file.value.hidden.map((id) => {
      const widget = drawnWindow.value?.widgets?.[id];
      return {
        id,
        text: widget ? widget.name || widget.type : (drawnCoat.value?.layers?.[id]?.texture ?? ""),
        detail: "",
      };
    }),
  },
]);
// a frame's value is its label, and the field, which no frame is called, draws the coat unframed
const frameOptions = computed(() => [
  { label: "Field", value: "Field" },
  ...(frames.value ?? []).map(({ label, place }) => ({
    label: place.origin === "vanilla" ? label : `${label} · ${workspace.originName(place.origin)}`,
    value: label,
  })),
]);
const framePlace = computed(() => frames.value?.find(({ label }) => label === store.frame)?.place);
const mask = computed(() =>
  framePlace.value ? { maskImage: `url("${fileUrl(framePlace.value.path)}")`, maskSize: "100% 100%" } : {},
);
const caretKey = computed(() => {
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  const widget = drawnWindow.value?.widgets?.[file.value.selected ?? -1];
  const places = drawnWindow.value?.places ?? [];
  const property = widget?.properties?.findLast(
    ({ place, range }) => places[place]?.key === model.place.key && Range.containsPosition(range, caret),
  );
  return split ? (property?.key.toLowerCase() ?? "") : "";
});
const menu = computed<ContextMenuItem[][]>(() => {
  const widgets = drawnWindow.value?.widgets ?? [];
  const widget = widgets[file.value.selected ?? -1];
  const place = widget && drawnWindow.value?.places?.[widget.place];
  const path = model.place.path;
  if (!widget || !place) return [];
  const parent = widget.id ? widgets[widget.parent] : undefined;
  return [
    [
      {
        label: parent ? `Select the parent, ${parent.name || parent.type}` : "Select the parent",
        icon: "i-lucide-corner-left-up",
        kbds: ["arrowup"],
        disabled: !parent,
        onSelect: () => store.select(path, widget.parent),
      },
      { label: "Hide", icon: "i-lucide-eye-off", kbds: ["H"], onSelect: () => store.hide(path, widget.id, true) },
      {
        label: "Show everything hidden",
        icon: "i-lucide-eye",
        kbds: ["shift", "H"],
        disabled: !file.value.hidden.length,
        onSelect: showHidden,
      },
    ],
    [
      {
        label: "Go to code",
        icon: "i-lucide-code",
        kbds: ["enter"],
        onSelect: () => store.showCode(place, widget.range),
      },
      ...(widget.typeDefined
        ? [{ label: "Open its type", icon: "i-lucide-layers", onSelect: () => void openType(place, widget.range) }]
        : []),
    ],
    [
      {
        label: "Copy its name",
        icon: "i-lucide-copy",
        onSelect: () => void Clipboard.SetText(widget.name || widget.type),
      },
      { label: "Copy its path", icon: "i-lucide-copy", onSelect: () => void Clipboard.SetText(place.path) },
    ],
  ];
});

watchPostEffect(() => {
  const element = floor.value;
  // the picture waits for the states, so what they hide never shows first
  if (!element || !drawnWindow.value || !states.value) return;
  const started = performance.now();
  const drawing = { hidden: hidden.value, choices: states.value.choices, rows: Number(settings.value("draw.rows")) };
  nodes.value = buildPicture(drawnWindow.value, drawing, element);
  windowRects.value = readRects(nodes.value, element);
  renderedMs.value = Math.round(performance.now() - started);
});
const readAgain = useDebounceFn(() => {
  if (floor.value) windowRects.value = readRects(nodes.value, floor.value);
}, 50);
useEventListener(floor, "load", () => void readAgain(), { capture: true });
// a pane hidden while the app's scale changed lays out again once shown
useResizeObserver(floor, () => void readAgain());

// the camera's pane takes the pointer and the keys, so a click off the picture selects nothing
const pane = computed(() => viewport.value?.pane);
useEventListener(pane, "click", applyClick);
useEventListener(pane, "contextmenu", openMenu);
useEventListener(pane, "keydown", applyKey);
useEventListener(pane, "pointermove", applyPointer);
useEventListener(pane, "pointerleave", leave);
useEventListener(
  () => document.fonts,
  "loadingdone",
  () => void readAgain(),
);

watchPostEffect(() => {
  const layers = drawnCoat.value?.layers?.filter((_, index) => !hidden.value.has(index)) ?? null;
  if (!canvas.value || !drawnCoat.value) return;
  const started = performance.now();
  void drawCoat(canvas.value, { ...drawnCoat.value, layers }, screen.value.height).then(() => {
    renderedMs.value = Math.round(performance.now() - started);
  });
});

watch(
  () => file.value.selected,
  async (id) => {
    await nextTick();
    const rect = id === null ? undefined : rects.value.get(id);
    if (rect) viewport.value?.reveal(rect);
  },
);

/** Finds how much the camera scales the picture. */
function findScale(): number {
  const element = isCoat.value ? canvas.value : floor.value;
  return element?.offsetWidth ? element.getBoundingClientRect().width / element.offsetWidth : 1;
}

/** Finds a point of the event in the pixels of the picture. */
function findPoint(event: MouseEvent): DOMPoint {
  const origin = (isCoat.value ? canvas.value : floor.value)?.getBoundingClientRect() ?? new DOMRect();
  const scale = findScale();
  return new DOMPoint((event.clientX - origin.left) / scale, (event.clientY - origin.top) / scale);
}

/** Finds the widgets or layers drawn under the pointer. A skin or a label stands for the widget it is part of. */
function findHits(event: MouseEvent): number[] {
  if (isCoat.value) {
    const point = findPoint(event);
    return [...boxes.value].filter(([, list]) => list.some((box) => contains(box, point))).map(([id]) => id);
  }
  const hits = new Set<number>();
  for (const element of document.elementsFromPoint(event.clientX, event.clientY)) {
    const id = element instanceof HTMLElement && floor.value?.contains(element) ? element.dataset.element : undefined;
    const widget = id === undefined ? undefined : drawnWindow.value?.widgets?.[Number(id)];
    if (widget) hits.add(widget.role === "skin" || widget.role === "label" ? widget.parent : widget.id);
  }
  return [...hits];
}

/** Whether a box holds a point. */
const contains = (box: DOMRect, point: DOMPoint) =>
  point.x >= box.left && point.x <= box.right && point.y >= box.top && point.y <= box.bottom;

/**
 * Finds the copy of a widget in a list's rows, or the instance of a layer, that the pointer is on: its place among the
 * boxes it covers. It is null for none.
 */
function findCopy(id: number | null, event: MouseEvent): number | null {
  const point = findPoint(event);
  const copy = id === null ? -1 : (boxes.value.get(id)?.findIndex((box) => contains(box, point)) ?? -1);
  return copy < 0 ? null : copy;
}

/** Sorts the hits at a point in the order clicks there go through them: smallest first, the topmost of equal ones. */
function sortHits(hits: number[]): number[] {
  const area = (id: number) => Math.min(...(boxes.value.get(id) ?? []).map((box) => box.width * box.height));
  return hits.toSorted((first, second) => area(first) - area(second));
}

/**
 * Finds the one hit that a click at a point selects: the one after the selected one in the order clicks go through
 * them, else the first, so clicks on one spot reach every widget drawn there. It is null for none.
 */
function findTarget(hits: number[]): number | null {
  const order = sortHits(hits);
  return order.length ? order[(order.indexOf(file.value.selected ?? -1) + 1) % order.length] : null;
}

/** Opens the definition of the type of the widget written in a file at the given range. */
async function openType(place: Place, range: Widget["range"]): Promise<void> {
  const [type] = (await lsp.Definition(place.path, range.startLineNumber, range.startColumn)) ?? [];
  if (type) store.showCode(type.place, type.range);
}

/** Makes a PNG of the coat as drawn, cut to the frame where one is chosen, its transparent pixels kept. */
async function snapshotCoat(): Promise<Blob | null> {
  const source = canvas.value;
  const sheet = document.createElement("canvas");
  const context = sheet.getContext("2d");
  if (!source || !context) return null;
  Object.assign(sheet, { width: source.width, height: source.height });
  context.drawImage(source, 0, 0);
  if (framePlace.value) {
    const frame = new Image();
    frame.src = fileUrl(framePlace.value.path);
    await frame.decode();
    context.globalCompositeOperation = "destination-in";
    context.drawImage(frame, 0, 0, sheet.width, sheet.height);
  }
  return new Promise((resolve) => sheet.toBlob(resolve, "image/png"));
}

/** Opens the code of the drawn drawable. */
function openDrawable(): void {
  const found = drawables.value?.find(({ name }) => name === drawable.value);
  if (found) store.showCode(model.place, found.range);
}

/** Shows every widget the session hides. */
function showHidden(): void {
  file.value.hidden.forEach((id) => store.hide(model.place.path, id, false));
}

/**
 * Selects what is clicked, the next widget under the pointer with each click on one spot, or nothing for a click off
 * the picture.
 */
function applyClick(event: MouseEvent): void {
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus({ preventScroll: true });
  const target = findTarget(findHits(event));
  store.select(model.place.path, target, findCopy(target, event));
}

/**
 * Selects what is right-clicked, whose menu then opens: the selected widget where it is under the pointer, else the
 * first.
 */
function openMenu(event: MouseEvent): void {
  const hits = sortHits(findHits(event));
  const target = hits.find((id) => id === file.value.selected) ?? hits.at(0);
  if (target !== undefined) store.select(model.place.path, target, findCopy(target, event));
}

/** Follows the pointer: it hovers what a click there would select. */
function applyPointer(event: PointerEvent): void {
  const target = findTarget(findHits(event));
  store.hover(model.place.path, target, findCopy(target, event));
}

/** Leaves the picture: nothing is under the pointer. */
function leave(): void {
  store.hover(model.place.path, null);
}

/** Acts on a key: Escape, the arrows, Enter, h, H or f. A key with Ctrl, Alt or Meta is left alone. */
function applyKey(event: KeyboardEvent): void {
  if (event.ctrlKey || event.altKey || event.metaKey) return;
  const path = model.place.path;
  const widgets = drawnWindow.value?.widgets ?? [];
  const places = drawnWindow.value?.places ?? [];
  const current = widgets[file.value.selected ?? -1];
  const siblings = widgets.filter(({ id, parent }) => id !== 0 && parent === current?.parent);
  const select = (id: number | undefined) => () => id !== undefined && store.select(path, id);
  const actions: Record<string, (() => void) | undefined> = {
    Escape: () => store.select(path, null),
    H: showHidden,
    ...(current && {
      Enter: () => store.showCode(places[current.place], current.range),
      h: () => store.hide(path, current.id, true),
      f: () => viewport.value?.fit(rects.value.get(current.id)),
      ArrowUp: select(current.id ? current.parent : undefined),
      ArrowDown: select(widgets.find(({ id, parent }) => id !== 0 && parent === current.id)?.id),
      ArrowLeft: select(siblings[siblings.indexOf(current) - 1]?.id),
      ArrowRight: select(siblings[siblings.indexOf(current) + 1]?.id),
    }),
  };
  const action = actions[event.key];
  if (!action) return;
  event.preventDefault();
  action();
}
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1">
    <USplitter
      :key="String(split)"
      :items="
        split
          ? [
              { id: 'code', slot: 'code', defaultSize: 35, minSize: 15 },
              { id: 'picture', slot: 'picture', defaultSize: 40, minSize: 20 },
              { id: 'panel', slot: 'panel', defaultSize: 25, minSize: 15 },
            ]
          : [
              { id: 'picture', slot: 'picture', defaultSize: 70, minSize: 30 },
              { id: 'panel', slot: 'panel', defaultSize: 30, minSize: 18 },
            ]
      "
      :auto-save-id="split ? 'pmt.draw.split' : 'pmt.draw.panes'"
      class="min-h-0 min-w-0 flex-1"
    >
      <template #code>
        <CodeEditor :model="model" active />
      </template>
      <template #picture>
        <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col">
          <div class="flex h-10 shrink-0 items-center gap-1.5 overflow-x-auto border-b border-default bg-muted px-2">
            <USelect
              v-if="!isCoat && resolutions.length"
              :model-value="resolution"
              :items="resolutions"
              icon="i-lucide-monitor"
              size="sm"
              class="w-36 shrink-0"
              @update:model-value="(value) => settings.write(resolutionKey, String(value))"
            />
            <USelect
              v-else-if="isCoat && frameOptions.length > 1"
              v-model="store.frame"
              placeholder="Field"
              :items="frameOptions"
              icon="i-lucide-shield"
              size="sm"
              class="w-36 shrink-0"
            />
            <USeparator orientation="vertical" class="mx-1 h-4" />
            <UTooltip v-if="!isCoat" text="Outline every widget's bounds">
              <UButton
                icon="i-lucide-square-dashed"
                square
                size="sm"
                :color="isBounded ? 'primary' : 'neutral'"
                :variant="isBounded ? 'soft' : 'ghost'"
                aria-label="Bounds"
                :aria-pressed="isBounded"
                @click="isBounded = !isBounded"
              />
            </UTooltip>
            <UTooltip v-if="!isCoat" text="The states that change what is drawn">
              <UButton
                icon="i-lucide-braces"
                :label="`States ${Object.keys(states?.applies ?? {}).length}`"
                size="sm"
                :color="file.tab === 'states' ? 'primary' : 'neutral'"
                :variant="file.tab === 'states' ? 'soft' : 'ghost'"
                @click="file.tab = 'states'"
              />
            </UTooltip>
          </div>
          <div class="relative flex min-h-0 flex-1 flex-col">
            <PictureViewport
              ref="viewport"
              :focus="isCoat ? null : (rects.get(0) ?? null)"
              :backdrop="isCoat"
              :snapshot="isCoat ? snapshotCoat : null"
              :title="drawable"
            >
              <UContextMenu :items="menu" :disabled="isCoat || file.hovered === null" :ui="{ content: 'w-80' }">
                <div
                  class="relative"
                  :class="!isCoat && 'bg-default'"
                  :style="isCoat ? undefined : { width: `${screen.width}px`, height: `${screen.height}px` }"
                >
                  <canvas v-if="isCoat" ref="canvas" class="block" :style="mask" />
                  <template v-else>
                    <div ref="floor" class="absolute inset-0 overflow-hidden" :class="{ 'pmt-bounds': isBounded }" />
                    <div
                      class="pointer-events-none absolute inset-0 border border-(--line-2)"
                      :style="{ borderWidth: 'calc(1px / var(--zoom, 1))' }"
                    >
                      <span
                        class="absolute bottom-full left-0 origin-bottom-left font-mono text-11 whitespace-nowrap text-(--fg-3)"
                        :style="{ transform: 'scale(calc(1 / var(--zoom, 1)))' }"
                      >
                        {{ screen.width }} × {{ screen.height }}
                      </span>
                    </div>
                  </template>
                  <WidgetMarks
                    :rects="rects"
                    :selected="file.selected"
                    :hovered="file.hovered"
                    :outlined="file.outlined"
                    :window="drawnWindow ?? NO_WINDOW"
                    :property="caretKey"
                  />
                </div>
              </UContextMenu>
              <template #toolbar>
                <span v-if="asyncStatus === 'loading'" class="flex items-center gap-1.5 px-1">
                  <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />Drawing
                </span>
                <span v-else-if="hasPicture" class="flex items-center gap-1.5 px-1">
                  <UIcon name="i-lucide-circle-check" class="size-3.5 text-success" />Rendered in {{ renderedMs }} ms
                </span>
                <UPopover
                  v-for="list in lists.filter(({ rows }) => rows.length)"
                  :key="list.label"
                  :content="{ side: 'top', align: 'start' }"
                >
                  <UButton
                    :label="`${list.rows.length} ${list.label}`"
                    :icon="list.icon"
                    trailing-icon="i-lucide-chevron-up"
                    :color="list.color"
                    variant="ghost"
                    size="xs"
                  />
                  <template #content>
                    <ul class="flex max-h-80 w-100 flex-col overflow-auto p-1 text-12">
                      <li
                        v-for="(row, ordinal) in list.rows"
                        :key="ordinal"
                        class="flex items-center rounded hover:bg-elevated"
                      >
                        <button
                          type="button"
                          class="flex min-w-0 grow flex-col px-1.5 py-1 text-start"
                          @click="store.select(model.place.path, row.id)"
                        >
                          <span class="truncate text-toned" :class="list.isHidden && 'font-mono'">{{ row.text }}</span>
                          <span v-if="row.detail" class="truncate font-mono text-11 text-dimmed">{{ row.detail }}</span>
                        </button>
                        <UButton
                          v-if="list.isHidden"
                          icon="i-lucide-eye"
                          color="neutral"
                          variant="ghost"
                          size="xs"
                          square
                          aria-label="Show"
                          @click="store.hide(model.place.path, row.id, false)"
                        />
                      </li>
                    </ul>
                  </template>
                </UPopover>
              </template>
            </PictureViewport>
            <div
              v-if="!hasPicture || isEmpty"
              class="pointer-events-none absolute inset-x-0 top-0 bottom-7 flex items-center justify-center p-6"
            >
              <UAlert
                v-if="error && !hasPicture"
                class="pointer-events-auto max-w-120"
                color="error"
                variant="subtle"
                icon="i-lucide-circle-x"
                :title="`${drawable} could not be drawn`"
                :description="error.message"
                :actions="[
                  {
                    label: 'Open in code',
                    icon: 'i-lucide-code',
                    color: 'neutral',
                    variant: 'outline',
                    onClick: openDrawable,
                  },
                  {
                    label: 'Try again',
                    icon: 'i-lucide-rotate-cw',
                    color: 'neutral',
                    variant: 'ghost',
                    onClick: () => void refetch(),
                  },
                ]"
              />
              <UEmpty
                v-else-if="isEmpty"
                icon="i-lucide-square-dashed"
                :title="`Nothing to draw: ${drawable}`"
                description="It has no size, texture, text or children of its own."
                size="sm"
              />
              <UEmpty
                v-else-if="!drawable"
                icon="i-lucide-app-window"
                title="No window"
                description="This file holds nothing to draw."
                size="sm"
              />
              <span v-else class="flex items-center gap-2 text-12 text-dimmed">
                <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />Drawing
              </span>
            </div>
          </div>
        </div>
      </template>
      <template #panel>
        <DrawDetail :model="model" :window="drawnWindow ?? null" :coat="drawnCoat ?? null" />
      </template>
    </USplitter>
    <CodeLink
      v-if="split && hasPicture"
      :model="model"
      :blocks="blocks"
      :boxes="markBoxes"
      :hidden="hidden"
      :missing="missing"
      :name="drawable"
    />
  </div>
</template>

<style>
.pmt-bounds [data-element] {
  outline: calc(1px / var(--zoom, 1)) dashed color-mix(in srgb, var(--link) 45%, transparent);
  outline-offset: calc(-1px / var(--zoom, 1));
}
</style>
